#/usr/bin/env sh

start_section() {
    echo -e "section_start:$(date +%s):${1}\r\e[0K${2}"
}

stop_section() {
    echo -e "section_end:$(date +%s):${1}\r\e[0K"
}
