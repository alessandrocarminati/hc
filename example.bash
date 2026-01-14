sshl() {
    local iport="1234"
    local sport="8080"
    local imode="1"
    local iverify="0"
    local nuse_gzip=false
    local OPTIND
    local hc_host=auto.hqhome163.com

    while getopts "p:m:v:z:s" opt; do
        case "$opt" in
            p) iport=$OPTARG ;;
            m) imode=$OPTARG ;;
            v) iverify=$OPTARG ;;
            z) nuse_gzip=true ;;
            s) sport=$OPTARG ;;

        esac
    done
    shift $((OPTIND-1))

    local cmd_base='export SESSIONID_=$(date +%Y%m%d.%H%M%S |sha1sum | cut -c1-8); '
    local log_content='$(date +%Y%m%d.%H%M%S) - ${SESSIONID_} - $(hostname --fqdn) [cwd=$(pwd)] > $(history -w /dev/stdout | tail -n1)'
    local func_defs="l4h(){ wget -qO- -- \"http://127.0.0.1:${sport}/export?grep1=\$1&color=always\"; }; export -f l4h;"

    local transport=""
    if [ "$imode" = "2" ]; then
        transport="socat - OPENSSL:127.0.0.1:${iport},verify=${iverify}"
    else
        transport="nc 127.0.0.1 ${iport}"
    fi

    local full_payload="${cmd_base}${func_defs} export PROMPT_COMMAND='if [ -z \"\$LAST_H_\" ]; then LAST_H_=\$(history 1| sed -r \"s/ *([0-9]+).*/\1/\"); elif [ \"\$(history 1| sed -r \"s/ *([0-9]+).*/\1/\")\" -ne \"\$LAST_H_\" ]; then LAST_H_=\"\$(history 1| sed -r \"s/ *([0-9]+).*/\1/\")\"; echo \"${log_content}\" | ${transport}; fi'; exec bash -i"

    local encoded_payload
    local remote_decode_cmd

    if [ "$nuse_gzip" = true ]; then
        encoded_payload=$(echo "$full_payload" | gzip -c | base64 | tr -d '\n')
        remote_decode_cmd="eval \$(echo ${encoded_payload} | base64 -d | gunzip -c)"
    else
        encoded_payload=$(echo "$full_payload" | base64 | tr -d '\n')
        remote_decode_cmd="eval \$(echo ${encoded_payload} | base64 -d)"
    fi

    ssh -t -R "${iport}:${hc_host}:${iport}" -R "${sport}:${hc_host}:${sport}" "$@" "$remote_decode_cmd"
}
