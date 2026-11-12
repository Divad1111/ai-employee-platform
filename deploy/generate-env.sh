#!/bin/sh
# 为首次安装生成 .env；兼容 macOS、Linux、群晖 DSM 的 POSIX sh，不依赖 Python。
set -eu
umask 077

fail() {
    printf '%s\n' "$*" >&2
    exit 1
}

usage() {
    printf '%s\n' 'Usage: sh generate-env.sh --domain HOST --server-image IMAGE --admin-image IMAGE [--output PATH]'
    printf '%s\n' 'Fresh installs only. Requires OpenSSL and awk. Existing files are never overwritten.'
}

script_dir=$(CDPATH= cd -P "$(dirname "$0")" && pwd)
domain=
server_image=
admin_image=
output="$script_dir/.env"
while [ "$#" -gt 0 ]; do
    case "$1" in
        --domain|--server-image|--admin-image|--output)
            [ "$#" -ge 2 ] || fail "Missing value for $1"
            case "$1" in
                --domain) domain=$2 ;;
                --server-image) server_image=$2 ;;
                --admin-image) admin_image=$2 ;;
                --output) output=$2 ;;
            esac
            shift 2
            ;;
        --help|-h) usage; exit 0 ;;
        *) fail "Unknown argument: $1" ;;
    esac
done

# 限制字符集，避免向 .env 注入变量展开、注释或额外配置行。
case "$domain" in
    ''|*[!A-Za-z0-9.-]*) fail 'Domain must be a plain DNS hostname or IPv4 address without scheme, port or path.' ;;
esac
command -v awk >/dev/null 2>&1 || fail 'awk is required.'
awk -v domain="$domain" 'BEGIN {
    if (length(domain) > 253) exit 1
    count = split(domain, labels, ".")
    for (i = 1; i <= count; i++)
        if (length(labels[i]) > 63 || labels[i] !~ /^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?$/) exit 1
}' || fail 'Domain contains an invalid hostname label.'
for reference in "$server_image" "$admin_image"; do
    case "$reference" in
        ''|[!A-Za-z0-9]*|*[!A-Za-z0-9._:/@-]*) fail 'Invalid image reference: use an image name with a tag or digest.' ;;
    esac
done
[ -n "$output" ] || fail 'Output path must not be empty.'
if [ -e "$output" ] || [ -L "$output" ]; then
    fail 'Output already exists; refusing to overwrite.'
fi
command -v openssl >/dev/null 2>&1 || fail 'OpenSSL is required. Install it or generate the file on another machine.'
template="$script_dir/.env.example"
[ -r "$template" ] || fail 'Cannot read .env.example next to this script.'

new_secret() {
    value=$(openssl rand -hex 32) || fail 'OpenSSL could not generate secure random bytes.'
    case "$value" in
        ''|*[!0-9a-f]*) fail 'OpenSSL returned an invalid random value.' ;;
    esac
    [ "${#value}" -eq 64 ] || fail 'OpenSSL returned an invalid random value length.'
    printf '%s' "$value"
}

# 每项独立生成，不通过命令行参数传递秘密，也不打印到日志。
password=$(new_secret)
secret_key=$(new_secret)
backup_key=$(new_secret)
setup_token=$(new_secret)
content=$(
    AIE_GEN_PASSWORD="$password" AIE_GEN_SECRET_KEY="$secret_key" \
    AIE_GEN_BACKUP_KEY="$backup_key" AIE_GEN_SETUP_TOKEN="$setup_token" \
    awk -v domain="$domain" -v server_image="$server_image" -v admin_image="$admin_image" '
    BEGIN {
        values["AIE_DOMAIN"] = domain
        values["AIE_PUBLIC_SERVER_URL"] = "https://" domain
        values["AIE_CONTROL_PLANE_IMAGE"] = server_image
        values["AIE_ADMIN_IMAGE"] = admin_image
        values["POSTGRES_PASSWORD"] = ENVIRON["AIE_GEN_PASSWORD"]
        values["AIE_SECRET_MASTER_KEY"] = ENVIRON["AIE_GEN_SECRET_KEY"]
        values["AIE_BACKUP_MASTER_KEY"] = ENVIRON["AIE_GEN_BACKUP_KEY"]
        values["AIE_SETUP_TOKEN"] = ENVIRON["AIE_GEN_SETUP_TOKEN"]
    }
    {
        sub(/\r$/, "")
        key = $0
        sub(/=.*/, "", key)
        if (key in values) print key "=" values[key]
        else print
    }' "$template"
)

# noclobber 防止并发生成覆盖原文件；umask 确保新文件权限为 0600。
if (set -C; printf '%s\n' "$content" > "$output"); then
    printf 'Created private environment file: %s\n' "$output"
else
    fail 'Cannot create output file; existing files are never overwritten.'
fi
