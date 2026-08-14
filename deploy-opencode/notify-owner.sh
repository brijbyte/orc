#!/bin/sh
# Notify the VM owner through every configured channel.
set -u

config=${NOTIFY_OWNER_CONFIG:-/etc/notify-owner.env}
[ -r "$config" ] || { echo "notify-owner: cannot read $config" >&2; exit 1; }
# This root-generated file contains only quoted variable assignments.
. "$config"

urgency=info
attachment=
while getopts 'u:f:' option; do
    case "$option" in
        u) urgency=$OPTARG ;;
        f) attachment=$OPTARG ;;
        *) echo 'usage: notify-owner [-u info|warn|urgent] [-f FILE] TITLE [BODY]' >&2; exit 2 ;;
    esac
done
shift $((OPTIND - 1))
if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
    echo 'usage: notify-owner [-u info|warn|urgent] [-f FILE] TITLE [BODY]' >&2
    exit 2
fi
if [ -n "$attachment" ] && [ ! -r "$attachment" ]; then
    echo "notify-owner: cannot read attachment: $attachment" >&2
    exit 1
fi

title=$1
if [ "$#" -eq 2 ]; then
    body=$2
elif [ -t 0 ]; then
    body=
else
    body=$(cat)
fi
case "$urgency" in
    info) priority=3; mark= ;;
    warn) priority=4; mark='WARNING: ' ;;
    urgent) priority=5; mark='URGENT: ' ;;
    *) echo "notify-owner: invalid urgency: $urgency" >&2; exit 2 ;;
esac

configured=0
failed=0
text=$mark$title
[ -z "$body" ] || text="$text

$body"
if [ -n "${TELEGRAM_BOT_TOKEN:-}" ] && [ -n "${TELEGRAM_CHAT_ID:-}" ]; then
    configured=1
    if [ -n "$attachment" ]; then
        if ! curl --fail --silent --show-error --max-time 60 \
            -F "chat_id=$TELEGRAM_CHAT_ID" -F "caption=$text" \
            -F "document=@$attachment" \
            "https://api.telegram.org/bot$TELEGRAM_BOT_TOKEN/sendDocument" >/dev/null; then
            echo 'notify-owner: Telegram delivery failed' >&2
            failed=1
        fi
    elif ! curl --fail --silent --show-error --max-time 10 \
        --data-urlencode "chat_id=$TELEGRAM_CHAT_ID" \
        --data-urlencode "text=$text" \
        "https://api.telegram.org/bot$TELEGRAM_BOT_TOKEN/sendMessage" >/dev/null; then
        echo 'notify-owner: Telegram delivery failed' >&2
        failed=1
    fi
fi

if [ -n "${NTFY_URL:-}" ]; then
    configured=1
    safe_title=$(printf '%s' "$mark$title" | tr '\r\n' '  ')
    set -- --fail --silent --show-error --max-time 60 \
        -H "Title: $safe_title" -H "Priority: $priority"
    [ -z "${NTFY_TOKEN:-}" ] || set -- "$@" -H "Authorization: Bearer $NTFY_TOKEN"
    if [ -n "$attachment" ]; then
        safe_body=$(printf '%s' "$body" | tr '\r\n' '  ')
        filename=$(basename "$attachment")
        set -- "$@" -H "Message: $safe_body" -H "Filename: $filename"
        if ! curl "$@" --data-binary "@$attachment" "$NTFY_URL" >/dev/null; then
            echo 'notify-owner: ntfy delivery failed' >&2
            failed=1
        fi
    elif ! printf '%s' "$body" | curl "$@" --data-binary @- "$NTFY_URL" >/dev/null; then
        echo 'notify-owner: ntfy delivery failed' >&2
        failed=1
    fi
fi

[ "$configured" -eq 1 ] || { echo 'notify-owner: no channel configured' >&2; exit 1; }
[ "$failed" -eq 0 ]
