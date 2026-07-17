#!/usr/bin/env python3

import base64
import json
import sys


def decode(value: str) -> str:
    return base64.b64decode(value).decode("utf-8") if value else ""


def attach(container, name, value):
    if isinstance(container, list):
        container.append(value)
    else:
        container[name] = value


root = {}
stack = [root]

with open(sys.argv[1], encoding="utf-8") as operations:
    for raw_line in operations:
        kind, encoded_name, encoded_value = raw_line.rstrip("\n").split("\t")
        name = decode(encoded_name)
        value = decode(encoded_value)
        if kind == "close":
            stack.pop()
            continue
        if kind == "object":
            child = {}
            attach(stack[-1], name, child)
            stack.append(child)
            continue
        if kind == "array":
            child = []
            attach(stack[-1], name, child)
            stack.append(child)
            continue
        if kind == "int":
            value = int(value)
        elif kind == "boolean":
            value = value not in ("", "0", "false", "False")
        attach(stack[-1], name, value)

json.dump(root, sys.stdout, ensure_ascii=False)
sys.stdout.write("\n")
