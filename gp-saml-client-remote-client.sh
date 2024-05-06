#!/bin/env bash
set -e

ARG0="$0"
ACTION="$1";

function usage() {
    echo "Usage: $ARG0 [connect|disconnect|status]"
    exit 1
}

function error() {
    >&2 echo "Error:" "${@}"
    exit 1
}

function run_curl_command() {
    set -e
    cat | ${@}
}

[ ! -f "$HOME/.gp-saml-client-remote-client-rc" ] || source "$HOME/.gp-saml-client-remote-client-rc"

GP_VPN_CONTAINER_SERVER="${GP_VPN_CONTAINER_SERVER:-http://localhost:8080}"
if [ -z "$GP_SAML_GUI_BIN" ]; then
    GP_SAML_GUI_BIN=$(which gp-saml-gui 2>/dev/null)
    [ ! -z "$GP_SAML_GUI_BIN" ] || error "Could not find path to gp-saml-gui, consider configuring it by setting \$GP_SAML_GUI_BIN"
else
    if ! which "$GP_SAML_GUI_BIN" 2>/dev/null 1>&2 ; then
        error "Invalid path to gp-saml-gui in \$GP_SAML_GUI_BIN: $GP_SAML_GUI_BIN"
    fi
fi

function maybe_prompt_for_value() {
    set -e
    local original_value="$2"
    local value
    read -p "$1 [$2]: " value
    if [ -z "$value" ]; then
        [ ! -z "$original_value" ] || error "No value provided for: $1"
        echo "$original_value"
    else
        echo "$value"
    fi
}

function send_get_req() {
    set -e
    run_curl_command '' curl -s -H "Content-Type: application/json" -X GET "$GP_VPN_CONTAINER_SERVER/$1"
}

function send_post_req() {
    set -e
    local STDIN=`cat`
    run_curl_command "$STDIN" curl -s -H "Content-Type: application/json" -X POST --data-binary @- "$GP_VPN_CONTAINER_SERVER/$1"
}

function req_disconnect() {
    set -e
    local result=$(echo -n "" | send_post_req 'disconnect' '')
    [ "$result" == "disconnected" ] || return 1
}

function build_connect_json_pw() {
    set -e
    jq -cn --arg pw "$1" --arg user "$OPENCONNECT_USER" --arg gw "$OPENCONNECT_GATEWAY" --arg gwdom "$OPENCONNECT_GATEWAY_DOMAIN" --argjson oc_addtl_args "$OPENCONNECT_ADDTL_ARGS" \
        '{"gw":$gw,"gwdom":$gwdom,"user":$user,"password":$pw,"oc-addtl-args":$oc_addtl_args}'
}

function build_connect_json_saml() {
    set -e
    jq -cn --arg saml "$1" --arg user "$OPENCONNECT_USER" --arg gw "$OPENCONNECT_GATEWAY" --arg gwdom "$OPENCONNECT_GATEWAY_DOMAIN" --argjson oc_addtl_args "$OPENCONNECT_ADDTL_ARGS" \
        '{"gw":$gw,"gwdom":$gwdom,"user":$user,"saml":$saml,"oc-addtl-args":$oc_addtl_args}'
}

function req_connect() {
    set -e
    local result=$(build_connect_json_pw "$1" | send_post_req 'connect')
    [ "$result" != "" ] || error "Connection request failed"
    local saml_url=$(echo "$result" | sed -n -r 's/^SAML-URL:(.*)$/\1/p')
    if [ "$saml_url" != "" ]; then
        local saml_response=$("$GP_SAML_GUI_BIN" -u "$saml_url")
        [ "$saml_response" != "" ] || error "Did not get SAML response"
        COOKIE=""
        #HOST=""
        USER=""
        OS=""
        eval "$saml_response"
        [ "$COOKIE" != "" ] || error "Did not get SAML cookie"
        #[ "$HOST" != "" ] || error "Did not get SAML host"
        [ "$USER" != "" ] || error "Did not get SAML user"
        [ "$OS" != "" ] || error "Did not get SAML os"
        OPENCONNECT_USER=${USER:-$OPENCONNECT_USER}
        OPENCONNECT_OS=${OS:-$OPENCONNECT_OS}
        #OPENCONNECT_GATEWAY_DOMAIN=${HOST:-$OPENCONNECT_GATEWAY_DOMAIN}
        result=$(build_connect_json_saml "$COOKIE" | send_post_req 'connect')
    fi

    echo "$result"
}

function req_status() {
    set -e
    local result=$(send_get_req 'status')
    [ "$result" != "" ] || error "Querying connection status failed"
    echo "$result"
}

function is_connected() {
    set -e
    local result=$(req_status)
    [ "$result" == "connected" ] || return 1
}

function prompt_for_password() {
    set -e
    local password
    read -s -p "Password: " password
    >&2 printf '\n' # read prints the prompt to stderr
    [ ! -z "$password" ] || return 1
    echo "$password"
}

function do_connect() {
    set -e
    local password=$(prompt_for_password)
    if [ "$password" != "" ]; then
        local result=$(req_connect "$password")
        [ ! -z "$result" ] || return 1
        echo "Status: $result"
    fi
}

case $ACTION in
    connect)
        if is_connected; then
            echo "Nothing to do, VPN is already connected"
        else
            OPENCONNECT_ADDTL_ARGS="${OPENCONNECT_ADDTL_ARGS:-[]}"
            OPENCONNECT_GATEWAY_DOMAIN=$(maybe_prompt_for_value "Gateway Domain" "$OPENCONNECT_GATEWAY_DOMAIN")
            OPENCONNECT_GATEWAY=$(maybe_prompt_for_value "Gateway" "$OPENCONNECT_GATEWAY")
            OPENCONNECT_USER=$(maybe_prompt_for_value "User" "$OPENCONNECT_USER")
            do_connect
        fi
        ;;
    disconnect)
        req_disconnect
        ;;
    status)
        echo "Status: $(req_status)"
        ;;
    *)
        usage
        ;;
esac
