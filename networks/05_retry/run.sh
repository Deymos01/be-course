#!/bin/sh
# Переименуйте в run.sh и укажите команду запуска своей программы.
set -e
cd "$(dirname "$0")"
exec ./bin/retry "$@"