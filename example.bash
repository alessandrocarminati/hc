# `sshl` is a wrapper function for the standard `ssh` command. It allows you
# to connect to a remote server and automatically inject logic that streams
# your shell history to a centralized **History Controller (HC) server**.
# ### Usage
# **`sshl [OPTIONS] [user@]hostname [-- SSH_ARGS]`**
# Use `--` to separate `sshl` specific options from standard `ssh` parameters
# (like port forwarding or identity files).
# ### Options
# |**Flag**|**Name**            |**Description**                                                            |
# |--------|--------------------|---------------------------------------------------------------------------|
# |`-p`    |**Ingestion Port**  |The port where history is sent. Defaults: `1234` (Cleartext), `1235` (SSL).|
# |`-s`    |**Search Port**     |The port for the search API. Defaults: `8080` (HTTP), `8445` (HTTPS).      |
# |`-m`    |**Ingestion Mode**  |Specify the ingestion protocol: `1` for Cleartext, `2` for SSL.            |
# |`-v`    |**Ingestion Verify**|(SSL only) Set to `1` to verify the server-side certificate, `0` to skip.  |
# |`-r`    |**Search Mode**     |Specify the search protocol: `1` for HTTP, `2` for HTTPS.                  |
# |`-V`    |**Search Verify**   |(HTTPS only) Set to `1` to verify the server-side certificate, `0` to skip.|
# |`-k`    |**API Key**         |Provide an API key to associate data with a specific tenant.               |
# |`-z`    |**Compression**     |Toggles whether the injection script is compressed before transmission.    |
# 
# ### Examples
# #### Basic connection
# Connects to a host using default settings (Implicit tenant, cleartext
# ingestion).
# ```
# $ sshl host.example.com
# ```
# #### Passing standard SSH parameters
# 
# Connects to a host on a custom SSH port (8022) using the `--`
# separator.
# ```
# $ sshl host.example.com -- -p 8022
# ```
# #### Advanced Secure Configuration
# Connects using SSL for ingestion (port 1235), HTTPS for search (port 8443),
# and a specific API Key. Verification is disabled in this example.
# ```
# $ sshl -p 1235 -s 8443 -m 2 -v 0 -r 2 -V 0 -k "hc_xxx.xxxx" host.example.com
# ```
# #### Installation
# To use this function, copy the source code into your `~/.bashrc` or `~/.zshrc`
# file and restart your terminal or run `source ~/.bashrc`.
# **Make sure `hc_host` points to the right hc instance.**

sshl() {
    local iport="1234" sport="8080" imode="1" iverify="0" rmode="1" rverify="1" apikey="" nuse_gzip=false OPTIND hc_host="hc.example.com"

    while getopts "p:s:m:v:r:V:k:zh" opt; do
        case "$opt" in
            p) iport=$OPTARG ;; s) sport=$OPTARG ;; m) imode=$OPTARG ;; v) iverify=$OPTARG ;;
            r) rmode=$OPTARG ;; V) rverify=$OPTARG ;; k) apikey=$OPTARG ;; z) nuse_gzip=true ;;
            h) echo "Usage: sshl [-p iport] [-s sport] [-m imode] [-v iverify] [-r rmode] [-V rverify] [-k apikey] [-z] user@host"; return 0 ;;
        esac
    done
    shift $((OPTIND-1))

    local cb='export SESSIONID_=$(date +%Y%m%d.%H%M%S|sha1sum|cut -c1-8);'
    local tr="nc 127.0.0.1 ${iport}"
    [ "$imode" = "2" ] && tr="socat - OPENSSL:127.0.0.1:${iport},verify=${iverify}"

    local dc="command -v"
    local chk_i='nc' && [ "$imode" = "2" ] && chk_i='socat'
    local lc='$(date +%Y%m%d.%H%M%S) - ${SESSIONID_} - $(hostname --fqdn) [cwd=$(pwd)] > '
    [ -n "$apikey" ] && lc="${lc}]apikey[${apikey}] "
    lc="${lc}"'$(history -w /dev/stdout|tail -n1)'

    local fd="l4h(){ "
    if [ "$rmode" = "2" ]; then
        local wo="" && [ "$rverify" = "0" ] && wo="--no-check-certificate "
        local ah="" && [ -n "$apikey" ] && ah="--header=\"Authorization: Bearer ${apikey}\" "
        fd="${fd}${dc} wget >/dev/null 2>&1||{ echo '[ERROR] wget N/A';return 1;};wget ${wo}${ah}\"https://127.0.0.1:${sport}/export?grep1=\$1&color=always\" -O - -q;"
    else
        fd="${fd}${dc} wget >/dev/null 2>&1||{ echo '[ERROR] wget N/A';return 1;};wget -qO- -- \"http://127.0.0.1:${sport}/export?grep1=\$1&color=always\";"
    fi
    fd="${fd}};export -f l4h;"

    local pc="${dc} ${chk_i} >/dev/null 2>&1&&export PROMPT_COMMAND='if [ -z \"\$LH\" ];then LH=\$(history 1|sed -r \"s/ *([0-9]+).*/\1/\");elif [ \"\$(history 1|sed -r \"s/ *([0-9]+).*/\1/\")\" -ne \"\$LH\" ];then LH=\$(history 1|sed -r \"s/ *([0-9]+).*/\1/\");echo \"${lc}\"|${tr};fi'||echo '[WARN] ${chk_i} N/A'"

    local fp="${cb}${fd}${pc};exec bash -i"
    local ep rdc
    if [ "$nuse_gzip" = true ]; then
        ep=$(echo "$fp"|gzip -c|base64|tr -d '\n')
        rdc="eval \$(echo ${ep}|base64 -d|gunzip -c)"
    else
        ep=$(echo "$fp"|base64|tr -d '\n')
        rdc="eval \$(echo ${ep}|base64 -d)"
    fi

    ssh -t -R "${iport}:${hc_host}:${iport}" -R "${sport}:${hc_host}:${sport}" "$@" "$rdc"
}
