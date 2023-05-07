# HC History Collector

This is a simple Golang application designed to receive 
and store command history data from remote clients. It 
listens for incoming connections using the net package 
in Golang, and accepts data in a simple text format.

When a client connects, the server reads the incoming 
data prints it on stdout. 

This application is designed to be lightweight and easy 
to use, with a minimal set of dependencies and no 
external libraries required. 

Typical usage is something like:
```
./hc >file.txt &
```

It can be compiled to run on any platform that is 
supported by Golang, including Windows, macOS, and Linux.

To get started with this application, simply download and 
compile the source code using the Go compiler. 

The app expects that History senders are configured with
something like
```
export PROMPT_COMMAND='echo "$(date +%Y%m%d.%H%M%S) - $(hostname -A) > $(history -w /dev/stdout | tail -n1)"|nc server.example.com 12345'
```
for example in their `.bashrc` file.


