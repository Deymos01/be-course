#!/bin/sh
# Запуск: ./run.sh <адрес-сервера> <порт>
if [ "$#" -ne 2 ]; then
    echo "usage: $0 <host> <port>" >&2
    exit 2
fi

exec go run main.go "$1" "$2"