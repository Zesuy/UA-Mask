#!/bin/sh

json_encode_field() {
    printf '%s' "$1" | base64 | tr -d '\n'
}

json_record() {
    local kind="$1"
    local name="$2"
    local value="$3"
    printf '%s\t%s\t%s\n' "$kind" "$(json_encode_field "$name")" "$(json_encode_field "$value")" >> "$JSON_OPS"
}

json_init() {
    : > "$JSON_OPS"
}

json_add_object() {
    json_record object "$1" ""
}

json_add_array() {
    json_record array "$1" ""
}

json_close_object() {
    json_record close "" ""
}

json_close_array() {
    json_record close "" ""
}

json_add_string() {
    json_record string "$1" "$2"
}

json_add_int() {
    json_record int "$1" "$2"
}

json_add_boolean() {
    json_record boolean "$1" "$2"
}

json_dump() {
    python3 "$JSON_RENDERER" "$JSON_OPS"
}
