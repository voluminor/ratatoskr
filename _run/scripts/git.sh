#!/bin/bash

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
hooks_dir="$(dirname "$script_dir")"
hooks_dir="$(dirname "$hooks_dir")"
hooks_dir="${hooks_dir}/.git/hooks"

#############################################################################
          ################[ Stylization ]################

style_nil=""

style_bold=""
style_underline=""

color_red=""
color_green=""
color_yellow=""
color_blue=""
color_magenta=""
color_cyan=""

if [ -t 1 ]; then
  style_nil=$(tput sgr0)
  style_bold=$(tput bold)
  style_underline=$(tput smul)
  color_red=$(tput setaf 1)
  color_green=$(tput setaf 2)
  color_yellow=$(tput setaf 3)
  color_blue=$(tput setaf 4)
  color_magenta=$(tput setaf 5)
  color_cyan=$(tput setaf 6)
fi

error() {
    echo "${style_bold}${color_red}ERROR:${style_nil}${color_red} $*${style_nil}"
}
info() {
    echo "${style_bold}${color_yellow}INFO:${style_nil}${color_yellow} $*${style_nil}"
}

red() {
    echo -e "${color_red}$*${style_nil}"
}
green() {
    echo -e "${color_green}$*${style_nil}"
}
yellow() {
    echo -e "${color_yellow}$*${style_nil}"
}
blue() {
    echo -e "${color_blue}$*${style_nil}"
}
magenta() {
    echo -e "${color_magenta}$*${style_nil}"
}
cyan() {
    echo -e "${color_cyan}$*${style_nil}"
}


#############################################################################
          ################[ Checking the environment ]################

# Проверяем наличие папки GIT
if [ ! -d "$hooks_dir" ]; then
    error "Folder '$hooks_dir' not found."
    exit 1
fi

#############################################################################
          ################[ command methods ]################

branch() {
  echo $(git branch | grep "*" | cut -c 3-)
  exit 0
}

hash() {
  local HASH=$( (printf "commit %s\0" $(git cat-file commit HEAD | wc -c); git cat-file commit HEAD) | sha1sum )
  local HASH=$(echo $HASH | cut -c -40)
  echo $HASH
  exit 0
}

tag(){
  local TAG=$(git describe --tags --abbrev=0)
  echo $TAG
  exit 0
}

##############

create_file(){
  local file_name=$1
  local file_hook=$2

  echo '#!/bin/bash' > "${hooks_dir}/$file_name"
  echo "bash _run/$file_hook" '$1' >> "${hooks_dir}/$file_name"

  chmod +x "${hooks_dir}/$file_name"
  info "ADD '$file_name'"
}

del_file(){
  local file_name=$1

  echo '#!/bin/bash' > "${hooks_dir}/$file_name"
  echo 'exit 0' >> "${hooks_dir}/$file_name"

  info "DEL '$file_name'"
}

add_commit(){
  create_file "commit-msg" "commit-hook.sh"
  exit 0
}

add_push(){
  create_file "pre-push" "push-hook.sh"
  exit 0
}

del_commit(){
  del_file "commit-msg"
  exit 0
}

del_push(){
  del_file "pre-push"
  exit 0
}

#############################################################################
          ################[ command processing ]################

help() {
    magenta "Available options:"
    blue "\t" --help       "\t${style_nil}Show help."
    yellow "\t" -b  --branch     "\t${style_nil}The name of the current active branch"
    yellow "\t" -h  --hash     "\t${style_nil}Hash of the current commit"
    yellow "\t" -t  --tag     "\t${style_nil}The last installed tag"
    green "\t" -ac  --add_commit     "\t${style_nil}Will add a hook to 'commit-msg'"
    green "\t" -ap  --add_push     "\t${style_nil}Will add a hook to 'pre-push'"
    red "\t" -dc  --del_commit     "\t${style_nil}Delete the hook from 'commit-msg'"
    red "\t" -dp  --del_push     "\t${style_nil}Delete the hook from 'pre-push'"

    exit 0
}

if [ $# -eq 0 ]; then
    error "Parameters not passed. Details ${color_blue}--help"
    exit 1
fi

while [[ "$#" -gt 0 ]]; do
    case "$1" in
        -b|--branch) branch ;;
        -h|--hash) hash ;;
        -t|--tag) tag ;;
        -ac|--add_commit) add_commit ;;
        -ap|--add_push) add_push ;;
        -dc|--del_commit) del_commit ;;
        -dp|--del_push) del_push ;;
        --help) help ;;
        *) error "Unknown parameter: ${color_blue}$1" ; exit 1 ;;
    esac
    shift
done

exit 1


