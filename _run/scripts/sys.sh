#!/bin/bash

declare -a required_values=("name.txt" "ver.txt")
declare -a required_scripts=("sys.sh" "git.sh")

#############################################################################
#############################################################################
increment_flag=false
increment_val_flag=false

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
values_dir="$(dirname "$script_dir")"
values_dir="$values_dir/values"

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

if [ ! -d "$values_dir" ]; then
    error "Folder '$values_dir' not found."
    exit 1
fi

for file_name in "${required_scripts[@]}"; do
    if [ ! -f "$script_dir/$file_name" ]; then
        error "The file '$script_dir/$file_name' is missing."
        exit 1
    fi
done

for file_name in "${required_values[@]}"; do
    if [ ! -f "$values_dir/$file_name" ]; then
        error "The file '$values_dir/$file_name' is missing."
        exit 1
    fi
done

#############################################################################
          ################[ version parsing ]################

global_ver=$(cat "${values_dir}/ver.txt" | cut -c -16)
ver_major=0
ver_minor=0
ver_patch=0

parse_ver() {
    local -a parts
    IFS='.' read -r -a parts <<< "$global_ver"

    ver_major="${parts[0]:-0}"
    ver_minor="${parts[1]:-0}"
    ver_patch="${parts[2]:-0}"
}
parse_ver

generate_ver() {
  echo "${ver_major}.${ver_minor}.${ver_patch}"
}

#############################################################################
          ################[ command methods ]################

major() {
  if [ ! "$increment_flag" = "true" ]; then
      echo $ver_major
      exit 0
  fi
}

minor() {
    if [ "$increment_flag" = "true" ]; then
        ((ver_minor++))
        ver_patch=0
        increment_val_flag=true
    else
        echo $ver_minor
        exit 0
    fi
}

patch() {
if [ "$increment_flag" = "true" ]; then
      ((ver_patch++))
      increment_val_flag=true
  else
      echo $ver_patch
      exit 0
  fi
}

name() {
    cat "$values_dir/name.txt" | cut -c -40
    exit 0
}

version() {
    generate_ver
    exit 0
}

#############################################################################
          ################[ command processing ]################

help() {
    magenta "Available options:"
    blue "\t" -h  --help       "\t${style_nil}Show help."
    yellow "\t" -n  --name     "\t${style_nil}Name function call."
    yellow "\t" -v  --ver      "\t${style_nil}Version function call."
    cyan "\t" -i  --increment  "\t${style_nil}Calls the increment function."
    cyan "\t" -ma --major      "\t${style_nil}Major function call."
    cyan "\t" -mi --minor      "\t${style_nil}Minor function call."
    cyan "\t" -pa --patch      "\t${style_nil}Call the patch function."
    exit 0
}

if [ $# -eq 0 ]; then
    error "Parameters not passed. Details ${color_blue}--help"
    exit 1
fi

while [[ "$#" -gt 0 ]]; do
    case "$1" in
        -i|--increment) increment_flag=true ;;
        -n|--name) name ;;
        -v|--ver) version ;;
        -ma|--major) major ;;
        -mi|--minor)  minor ;;
        -pa|--patch)  patch ;;
        -h|--help) help ;;
        *) error "Unknown parameter: ${color_yellow}$1" ; exit 1 ;;
    esac
    shift
done

if [[ "$increment_flag" = "true" && "$increment_val_flag" == "true" ]]; then
    global_ver=$(generate_ver)
    echo "$global_ver" > "${values_dir}/ver.txt"
    echo $global_ver
    exit 0
else
    if [[ "$increment_val_flag" == "true" ]]; then
      error "Used only with parameter: ${color_yellow}--increment"
    else
      error "Used only with parameters: ${color_yellow}--minor, --patch"
    fi
fi

exit 1