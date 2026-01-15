For a long time, my daily workflow looked like this:  
SSH into a server… do something clever… forget it… SSH into _another_ server… regret everything.

I work in an environment where servers are **borrowed from a pool**. You get one, you use it, and sooner ~~or later~~ you give it back. This sounds efficient, but it creates a very specific kind of pain: every time I landed on a “new” machine, all my carefully crafted commands in the history were gone.

And of course, the command I needed was _that one_. The long one. The ugly one. The one that worked only once, three months ago, at 2 a.m.

A configuration management tool _could_ probably handle this. In theory. But my reality is a bit messier.

The servers I use are usually borrowed, automatically installed, and destined to disappear again. I didn’t want to “improve” them by leaving behind shell glue and half-forgotten tweaks. Not because someone might reuse them,  but because someone else would have to clean them up.

On top of that, many of these machines live behind VPNs that really don’t want to talk to the outside world or the collector living in my home lab. If SSH works, I’m happy. If it needs anything more than that, it’s already too much.

I wanted something different:
-   no agent
-   no permanent changes
-   no files left behind
-   no assumptions about the remote network

In short: _leave no trace_.

## How hc was born

This is how **hc (History Collector)** started.

The very first version was a small [netcat hack](https://carminatialessandro.blogspot.com/2023/05/never-lose-command-again-how-to.html) in 2023. It worked… barely. But the idea behind it was solid, so I kept iterating. Eventually, it grew into a proper Go service with a SQL backend... (Postgres for today)

The core idea of hc is simple:

> **The remote machine should not need to know anything about the collector.**

No agent. No configuration file. No outbound connectivity.  
Instead, the trick is an **SSH reverse tunnel**.

From my laptop, I open an SSH session like this:
-   a reverse tunnel exposes a local port _on the remote machine_
-   that port points back to my hc service
-   from the remote shell’s point of view, the collector is just `127.0.0.1`

This was the “aha!” moment.

Because the destination is always `localhost`, the injected logging payload is **always identical**, no matter which server I connect to. The shell doesn’t know it’s talking to a central service... and it doesn’t care.

----------

## Injecting history without leaving scars

When I connect, I inject a small shell payload before starting the interactive session. This payload:
-   generates a session ID
-   defines helper functions
-   installs a `PROMPT_COMMAND` hook
-   forwards command history through the tunnel

Nothing is written to disk. When the SSH session ends, everything disappears.

A typical ingested line looks like this:
```
20240101.120305 - a1b2c3d4 - host.example.com [cwd=/root] > ls -la
```

This tells me:
-   when the command ran
-   from which host
-   in which directory
-   and what I actually typed

It turns out this is _surprisingly useful_ when you manage many machines and your memory is… optimistic.

## Minimal ingestion, flexible transport

hc is intentionally boring when it comes to ingestion... and I mean that as a compliment.

On the client side, it’s just standard Unix plumbing:
-   `nc` for plaintext logging on trusted networks
-   `socat` for TLS when you need encryption

No custom protocol, no magic framing. Just lines over a pipe.

This also makes debugging very easy. If something breaks, you can literally `cat` the traffic.

## Multi-tenancy without leaking secrets

Security became more important as hc grew.

I wanted one collector, multiple users, and no accidental data mixing. hc supports:
-   TLS client certificates
-   API keys

For API keys, I chose a slightly unusual format:

`]apikey[key.secret]` 

The server detects this pattern **in memory**, uses it to identify the tenant, and then **removes it immediately**. The stripped command is what gets stored — both in the database and in the append-only spool.

This way:
-   secrets never hit disk
-   grep output never leaks credentials
-   logs stay safe to share

## Searching is a different problem (and that’s good)

Ingestion and retrieval are intentionally separate.

When I want to _find_ a command, hc exposes a simple HTTP(S) GET endpoint. I deliberately chose GET instead of POST because it plays nicely with the Unix philosophy.

Example:
```
wget \
  --header="Authorization: Bearer my_key" \ "https://hc.example.com/export?grep1=docker&color=always" \
  -O - | grep prune
```

This feels natural. hc becomes just another tool in the pipeline.

## Shell archaeology: BusyBox, ash, and PS1 tricks

Working on hc also sent me down some unexpected rabbit holes.

For example: BusyBox `ash` doesn’t support `PROMPT_COMMAND`. Last year, I shared a [workaround](https://carminatialessandro.blogspot.com/2025/06/logging-shell-commands-in-busybox-yes.html) on Hacker News that required patching the shell at source level.

Then a user named _tyingq_ showed me something clever:  
you can embed **runtime-evaluated expressions inside `PS1`**, like:
```
PS1="\$(date) $ "
```

That expression is executed every time the prompt is rendered.

I’m currently experimenting with this approach to replace my previous patching strategy. If it works well enough, hc moves one step closer to being **truly zero-artifact on every shell**.

## Where to find it (and what’s next)

You can find the source code, and BusyBox research notes.

Right now, I’m working on:
-   a SQLite backend for single-user setups
-   more shell compatibility testing
-   better documentation around injection payloads

If you have opinions about:
-   the `]apikey[` stripping logic
-   using `PS1` for high-volume logging
-   or weird shells I should test next

…I’d genuinely love to hear them.
