sshl() {
    local port="12345"
    local mode="1"
    local verify="0"
    local use_gzip=false
    local OPTIND
    local hc_host=wiki-ng.hqhome163.com

    while getopts "p:m:v:z" opt; do
        case "$opt" in
            p) port=$OPTARG ;;
            m) mode=$OPTARG ;;
            v) verify=$OPTARG ;;
            z) use_gzip=true ;;
        esac
    done
    shift $((OPTIND-1))

    local cmd_base='export SESSIONID_=$(date +%Y%m%d.%H%M%S |sha1sum | cut -c1-8); '
    local log_content='$(date +%Y%m%d.%H%M%S) - ${SESSIONID_} - $(hostname --fqdn) [cwd=$(pwd)] > $(history -w /dev/stdout | tail -n1)'

    local transport=""
    if [ "$mode" = "2" ]; then
        transport="socat - OPENSSL:127.0.0.1:${port},verify=${verify}"
    else
        transport="nc 127.0.0.1 ${port}"
    fi

    local full_payload="${cmd_base} export PROMPT_COMMAND='echo \"${log_content}\" | ${transport}'; exec bash -i"

    local encoded_payload
    local remote_decode_cmd

    if [ "$use_gzip" = true ]; then
        encoded_payload=$(echo "$full_payload" | gzip -c | base64 | tr -d '\n')
        remote_decode_cmd="eval \$(echo ${encoded_payload} | base64 -d | gunzip -c)"
    else
        encoded_payload=$(echo "$full_payload" | base64 | tr -d '\n')
        remote_decode_cmd="eval \$(echo ${encoded_payload} | base64 -d)"
    fi

    ssh -t -R "${port}:${hc_host}:${port}" "$@" "$remote_decode_cmd"
}
