# shellcheck shell=bash

# shellcheck disable=SC2034
declare -Ag MFA_TOTP_SECRETS=(
    ["admin@example.com"]="K745D33R6A3NCWP5C3NYDQMBQF5ZFFHU"
    ["deployer@example.com"]="KB6VQXGZLMN4Y3DC"
    ["viewer@example.com"]="NB2WY5DPFVXHI6ZT"
)

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
