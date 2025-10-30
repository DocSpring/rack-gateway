# shellcheck shell=bash

# Avoid bash associative arrays for portability; provide a lookup function instead
get_totp_secret() {
    # $1 = user email
    case "$1" in
        "admin@example.com") echo "K745D33R6A3NCWP5C3NYDQMBQF5ZFFHU" ;;
        "deployer@example.com") echo "KB6VQXGZLMN4Y3DC" ;;
        "viewer@example.com") echo "NB2WY5DPFVXHI6ZT" ;;
        *) echo "" ;;
    esac
}

generate_totp_code() {
    local secret="$1"
    SECRET="$secret" python3 <<'PY'
import base64, hashlib, hmac, os, struct, time

secret = os.environ["SECRET"].strip().replace(' ', '').upper()
key = base64.b32decode(secret)
counter = int(time.time() // 30)
msg = struct.pack('>Q', counter)
digest = hmac.new(key, msg, hashlib.sha1).digest()
offset = digest[-1] & 0x0F
code = (struct.unpack('>I', digest[offset:offset + 4])[0] & 0x7FFFFFFF) % 1000000
print(f"{code:06d}")
PY
}

clear_mfa_replay_protection() {
    psql_exec "DELETE FROM used_totp_steps; DELETE FROM mfa_attempts;"
}
