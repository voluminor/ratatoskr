#!/usr/bin/env bash
set -Eeuo pipefail

OUT="${OUT:-/out}/throughput"
WARMUP_SECONDS="${THROUGHPUT_WARMUP_SECONDS:-5}"
MEASURE_SECONDS="${THROUGHPUT_MEASURE_SECONDS:-20}"
REPETITIONS="${THROUGHPUT_REPETITIONS:-3}"
PROFILE_SECONDS="${THROUGHPUT_PROFILE_SECONDS:-10}"
TCP_PAYLOAD="${THROUGHPUT_TCP_PAYLOAD:-262144}"
UDP_PAYLOAD="${THROUGHPUT_UDP_PAYLOAD:-1200}"
STABILITY_WARN_PERCENT="${THROUGHPUT_STABILITY_WARN_PERCENT:-10}"
read -r -a STREAMS <<< "${THROUGHPUT_STREAMS:-1 4 16}"
read -r -a REQUESTED_CPU_VALUES <<< "${THROUGHPUT_CPU_VALUES:-1 2 4 8}"

ORIGINAL_GOMAXPROCS_A=""
ORIGINAL_GOMAXPROCS_B=""
RESTORED_GOMAXPROCS=0
CURRENT_STAGE="initialization"
CONDITION_FILES=()
RUN_STARTED_AT=$(date -u +%Y-%m-%dT%H:%M:%SZ)
RUN_STARTED_EPOCH=$(date +%s)

require_integer() {
  local name=$1 value=$2 minimum=$3 maximum=$4
  if ! [[ "${value}" =~ ^[0-9]+$ ]] || [ "${value}" -lt "${minimum}" ] || [ "${value}" -gt "${maximum}" ]; then
    echo "${name} must be an integer between ${minimum} and ${maximum}" >&2
    return 1
  fi
}

validate_config() {
  require_integer THROUGHPUT_WARMUP_SECONDS "${WARMUP_SECONDS}" 1 30
  require_integer THROUGHPUT_MEASURE_SECONDS "${MEASURE_SECONDS}" 1 30
  require_integer THROUGHPUT_REPETITIONS "${REPETITIONS}" 1 10
  require_integer THROUGHPUT_PROFILE_SECONDS "${PROFILE_SECONDS}" 6 30
  require_integer THROUGHPUT_TCP_PAYLOAD "${TCP_PAYLOAD}" 1 1048576
  require_integer THROUGHPUT_UDP_PAYLOAD "${UDP_PAYLOAD}" 1 65463
  require_integer THROUGHPUT_STABILITY_WARN_PERCENT "${STABILITY_WARN_PERCENT}" 1 100
  if [ "${#STREAMS[@]}" -eq 0 ] || [ "${#REQUESTED_CPU_VALUES[@]}" -eq 0 ]; then
    echo 'throughput stream and CPU lists must not be empty' >&2
    return 1
  fi
  local previous=0 value found_one=0
  for value in "${STREAMS[@]}"; do
    require_integer THROUGHPUT_STREAMS "${value}" 1 32
    if [ "${value}" -le "${previous}" ]; then
      echo 'THROUGHPUT_STREAMS must be strictly increasing' >&2
      return 1
    fi
    previous=${value}
  done
  previous=0
  for value in "${REQUESTED_CPU_VALUES[@]}"; do
    require_integer THROUGHPUT_CPU_VALUES "${value}" 1 1024
    if [ "${value}" -le "${previous}" ]; then
      echo 'THROUGHPUT_CPU_VALUES must be strictly increasing' >&2
      return 1
    fi
    if [ "${value}" -eq 1 ]; then
      found_one=1
    fi
    previous=${value}
  done
  if [ "${found_one}" -ne 1 ]; then
    echo 'THROUGHPUT_CPU_VALUES must include 1 for speedup calculation' >&2
    return 1
  fi
}

get_json() {
  curl -fsS --max-time "${3:-10}" "http://$1:$2"
}

post_json() {
  curl -fsS --max-time "${4:-20}" -H 'Content-Type: application/json' -d "$3" "http://$1:$2"
}

wait_health() {
  local node=$1 deadline=$((SECONDS + 240))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    if get_json "${node}" '8080/health' 3 > "${OUT}/${node}-health.json" 2>/dev/null; then
      return 0
    fi
    sleep 2
  done
  return 1
}

new_run_id() {
  tr -d '-' < /proc/sys/kernel/random/uuid
}

case_address() {
  local transport=$1 network=$2
  if [ "${transport}" = direct ]; then
    if [ "${network}" = tcp ]; then
      echo 'node-b:19080'
    else
      echo 'node-b:19081'
    fi
    return
  fi
  jq -r ".${network}_throughput_addr" "${OUT}/node-b-health.json"
}

capture_runtime() {
  local directory=$1 suffix=$2
  get_json node-a '8080/runtime' 10 > "${directory}/sender-runtime-${suffix}.json"
  get_json node-b '8080/runtime' 10 > "${directory}/receiver-runtime-${suffix}.json"
}

capture_profiles() {
  local directory=$1
  curl -fsS --max-time 10 'http://node-a:7070/debug/pprof/profile?seconds=3' -o "${directory}/sender-cpu.pprof" &
  local sender_cpu=$!
  curl -fsS --max-time 10 'http://node-b:7070/debug/pprof/profile?seconds=3' -o "${directory}/receiver-cpu.pprof" &
  local receiver_cpu=$!
  wait "${sender_cpu}"
  wait "${receiver_cpu}"

  curl -fsS --max-time 10 'http://node-a:7070/debug/pprof/trace?seconds=1' -o "${directory}/sender-trace.out" &
  local sender_trace=$!
  curl -fsS --max-time 10 'http://node-b:7070/debug/pprof/trace?seconds=1' -o "${directory}/receiver-trace.out" &
  local receiver_trace=$!
  wait "${sender_trace}"
  wait "${receiver_trace}"
}

set_gomaxprocs() {
  local value=$1 directory=$2 request
  request=$(jq -nc --argjson value "${value}" '{value:$value}')
  mkdir -p "${directory}"
  post_json node-a '8080/runtime/gomaxprocs' "${request}" 10 > "${directory}/node-a.json"
  post_json node-b '8080/runtime/gomaxprocs' "${request}" 10 > "${directory}/node-b.json"
  jq -se --argjson value "${value}" 'all(.[]; .ok == true and .current == $value and .overridden == true)' \
    "${directory}/node-a.json" "${directory}/node-b.json" >/dev/null
}

restore_gomaxprocs() {
  if [ "${RESTORED_GOMAXPROCS}" -eq 1 ]; then
    return
  fi
  local directory="${OUT}/gomaxprocs/restore"
  mkdir -p "${directory}"
  if [ -n "${ORIGINAL_GOMAXPROCS_A}" ]; then
    post_json node-a '8080/runtime/gomaxprocs' '{"restore":true}' 10 > "${directory}/node-a.json"
  fi
  if [ -n "${ORIGINAL_GOMAXPROCS_B}" ]; then
    post_json node-b '8080/runtime/gomaxprocs' '{"restore":true}' 10 > "${directory}/node-b.json"
  fi
  jq -se 'all(.[]; .ok == true and .overridden == false)' "${directory}/node-a.json" "${directory}/node-b.json" >/dev/null
  RESTORED_GOMAXPROCS=1
}

cleanup() {
  local rc=$?
  restore_gomaxprocs || true
  if [ "${rc}" -ne 0 ]; then
    jq -nc --arg stage "${CURRENT_STAGE}" --argjson exit_code "${rc}" \
      '{ok:false,stage:$stage,exit_code:$exit_code}' > "${OUT}/failure.json" 2>/dev/null || true
  fi
}

compose_result() {
  local sender=$1 receiver=$2 output=$3 gomaxprocs=$4 repetition=$5
  jq -n --slurpfile sender "${sender}" --slurpfile receiver "${receiver}" \
    --argjson gomaxprocs "${gomaxprocs}" --argjson repetition "${repetition}" '
    ($sender[0]) as $s |
    ($receiver[0]) as $r |
    (($s.duration_ms // 0) / 1000) as $duration |
    (($s.sent_packets // 0) - ($r.received_packets // 0)) as $raw_loss |
    {
      ok: (($s.ok == true) and ($r.ok == true) and (($r.received_bytes // 0) > 0)),
      transport: $s.transport,
      network: $s.network,
      address: $s.address,
      gomaxprocs: $gomaxprocs,
      repetition: $repetition,
      streams: $s.streams,
      payload_bytes: $s.payload_bytes,
      duration_ms: $s.duration_ms,
      sent_bytes: $s.sent_bytes,
      received_bytes: $r.received_bytes,
      sent_packets: $s.sent_packets,
      received_packets: $r.received_packets,
      sender_mib_per_s: $s.mebibytes_per_s,
      receiver_mib_per_s: (if $duration > 0 then ($r.received_bytes / $duration / 1048576) else 0 end),
      sender_packets_per_s: $s.packets_per_s,
      receiver_packets_per_s: (if $duration > 0 then (($r.received_packets // 0) / $duration) else 0 end),
      loss_packets: (if $s.network == "udp" then (if $raw_loss > 0 then $raw_loss else 0 end) else null end),
      loss_percent: (if $s.network == "udp" and $s.sent_packets > 0 then ((if $raw_loss > 0 then $raw_loss else 0 end) * 100 / $s.sent_packets) else null end),
      duplicates: $r.duplicates,
      reordered: $r.reordered,
      too_old: $r.too_old,
      sender_errors: $s.errors,
      sender_last_error: $s.last_error
    }
  ' > "${output}"
}

run_once() {
  local transport=$1 network=$2 streams=$3 seconds=$4 directory=$5 profiled=$6 gomaxprocs=$7 repetition=$8
  local run_id address payload control request sender receiver
  run_id=$(new_run_id)
  address=$(case_address "${transport}" "${network}")
  payload="${UDP_PAYLOAD}"
  if [ "${network}" = tcp ]; then
    payload="${TCP_PAYLOAD}"
  fi
  mkdir -p "${directory}"
  control=$(jq -nc --arg id "${run_id}" --arg transport "${transport}" --arg network "${network}" \
    '{id:$id,transport:$transport,network:$network}')
  request=$(jq -nc --arg id "${run_id}" --arg transport "${transport}" --arg network "${network}" \
    --arg address "${address}" --argjson seconds "${seconds}" --argjson streams "${streams}" --argjson payload "${payload}" \
    '{id:$id,transport:$transport,network:$network,address:$address,seconds:$seconds,streams:$streams,payload_bytes:$payload}')
  sender="${directory}/sender.json"
  receiver="${directory}/receiver.json"

  post_json node-b '8080/throughput/start' "${control}" 10 > "${directory}/start.json"
  curl -sS --max-time "$((seconds + 10))" -H 'Content-Type: application/json' -d "${request}" \
    'http://node-a:8080/throughput/run' > "${sender}" &
  local sender_pid=$!
  local profile_ok=1 sender_ok=1
  if [ "${profiled}" -eq 1 ]; then
    sleep 1
    if ! capture_profiles "${directory}"; then
      profile_ok=0
    fi
  fi
  if ! wait "${sender_pid}"; then
    sender_ok=0
  fi
  sleep 1
  post_json node-b '8080/throughput/finish' "${control}" 10 > "${receiver}"
  compose_result "${sender}" "${receiver}" "${directory}/result.json" "${gomaxprocs}" "${repetition}"
  [ "${profile_ok}" -eq 1 ] && [ "${sender_ok}" -eq 1 ] && jq -e '.ok == true' "${directory}/result.json" >/dev/null
}

aggregate_condition() {
  local transport=$1 network=$2 streams=$3 gomaxprocs=$4 directory=$5
  jq -s --arg transport "${transport}" --arg network "${network}" --argjson streams "${streams}" \
    --argjson gomaxprocs "${gomaxprocs}" --argjson expected "${REPETITIONS}" \
    --argjson warn "${STABILITY_WARN_PERCENT}" '
    def stats($values):
      ($values | sort) as $v |
      ($v | length) as $n |
      (if ($n % 2) == 1 then $v[($n / 2 | floor)] else (($v[$n / 2 - 1] + $v[$n / 2]) / 2) end) as $median |
      {
        samples: $n,
        min: $v[0],
        max: $v[-1],
        median: $median,
        mean: (($v | add) / $n),
        relative_range_percent: (if $median > 0 then (($v[-1] - $v[0]) * 100 / $median) else 0 end)
      };
    . as $runs |
    stats($runs | map(.receiver_mib_per_s)) as $receiver |
    stats($runs | map(.sender_mib_per_s)) as $sender |
    stats($runs | map(.receiver_packets_per_s)) as $packets |
    {
      ok: (($runs | length) == $expected and all($runs[]; .ok == true)),
      transport: $transport,
      network: $network,
      streams: $streams,
      gomaxprocs: $gomaxprocs,
      receiver_mib_per_s: $receiver,
      sender_mib_per_s: $sender,
      receiver_packets_per_s: $packets,
      loss_percent: (if $network == "udp" then stats($runs | map(.loss_percent)) else null end),
      warnings: (if $receiver.relative_range_percent > $warn then ["\($network)-\($transport) streams=\($streams) GOMAXPROCS=\($gomaxprocs): receiver throughput relative range exceeds stability threshold"] else [] end),
      runs: $runs
    }
  ' "${directory}"/repeat-*/result.json > "${directory}/condition.json"
  jq -e '.ok == true' "${directory}/condition.json" >/dev/null
  CONDITION_FILES+=("${directory}/condition.json")
}

run_condition_pair() {
  local network=$1 streams=$2 gomaxprocs=$3 root=$4
  local direct_dir="${root}/${network}-direct/streams-${streams}"
  local ygg_dir="${root}/${network}-ygg/streams-${streams}"
  CURRENT_STAGE="${network} streams=${streams} GOMAXPROCS=${gomaxprocs} warm-up"
  run_once direct "${network}" "${streams}" "${WARMUP_SECONDS}" "${direct_dir}/warmup" 0 "${gomaxprocs}" 0
  run_once ygg "${network}" "${streams}" "${WARMUP_SECONDS}" "${ygg_dir}/warmup" 0 "${gomaxprocs}" 0

  local repetition transport
  for ((repetition = 1; repetition <= REPETITIONS; repetition++)); do
    local order=(direct ygg)
    if ((repetition % 2 == 0)); then
      order=(ygg direct)
    fi
    for transport in "${order[@]}"; do
      CURRENT_STAGE="${network}-${transport} streams=${streams} GOMAXPROCS=${gomaxprocs} repetition=${repetition}"
      if [ "${transport}" = direct ]; then
        run_once direct "${network}" "${streams}" "${MEASURE_SECONDS}" "${direct_dir}/repeat-${repetition}" 0 "${gomaxprocs}" "${repetition}"
      else
        run_once ygg "${network}" "${streams}" "${MEASURE_SECONDS}" "${ygg_dir}/repeat-${repetition}" 0 "${gomaxprocs}" "${repetition}"
      fi
    done
  done
  aggregate_condition direct "${network}" "${streams}" "${gomaxprocs}" "${direct_dir}"
  aggregate_condition ygg "${network}" "${streams}" "${gomaxprocs}" "${ygg_dir}"
}

select_baseline() {
  local network=$1 transport=$2 root=${3:-"${OUT}/baseline"}
  local directory="${root}/${network}-${transport}"
  CURRENT_STAGE="select baseline ${network}-${transport}"
  jq -s 'max_by(.receiver_mib_per_s.median)' "${directory}"/streams-*/condition.json > "${directory}/selected.json"
  jq -e '.ok == true' "${directory}/selected.json" >/dev/null
}

build_scaling_point() {
  local network=$1 gomaxprocs=$2 streams=$3 root=$4 output=$5
  jq -n --slurpfile direct "${root}/${network}-direct/streams-${streams}/condition.json" \
    --slurpfile ygg "${root}/${network}-ygg/streams-${streams}/condition.json" \
    --arg network "${network}" --argjson gomaxprocs "${gomaxprocs}" --argjson streams "${streams}" '
    ($direct[0]) as $d | ($ygg[0]) as $y |
    {
      ok: ($d.ok and $y.ok),
      network: $network,
      gomaxprocs: $gomaxprocs,
      streams: $streams,
      direct: $d,
      ygg: $y,
      ygg_percent_of_direct: (if $d.receiver_mib_per_s.median > 0 then ($y.receiver_mib_per_s.median * 100 / $d.receiver_mib_per_s.median) else 0 end),
      overhead_percent: (if $d.receiver_mib_per_s.median > 0 then (100 - ($y.receiver_mib_per_s.median * 100 / $d.receiver_mib_per_s.median)) else 0 end)
    }
  ' > "${output}"
}

build_scaling_summary() {
  local output=$1
  shift
  jq -s '
    sort_by(.gomaxprocs) as $points |
    ([$points[] | select(.gomaxprocs == 1)][0].ygg.receiver_mib_per_s.median) as $base |
    $points | map(
      (.ygg.receiver_mib_per_s.median / $base) as $speedup |
      . + {
        ygg_speedup_vs_1_cpu: $speedup,
        scaling_efficiency_percent: ($speedup * 100 / .gomaxprocs)
      }
    )
  ' "$@" > "${output}"
}

profile_case() {
  local transport=$1 network=$2 streams=$3 gomaxprocs=$4 directory=$5
  CURRENT_STAGE="profile ${network}-${transport} streams=${streams} GOMAXPROCS=${gomaxprocs}"
  mkdir -p "${directory}"
  capture_runtime "${directory}" before
  run_once "${transport}" "${network}" "${streams}" "${PROFILE_SECONDS}" "${directory}" 1 "${gomaxprocs}" 0
  capture_runtime "${directory}" after
}

self_test() {
  local root="${OUT}/self-test" condition="${OUT}/self-test/condition"
  rm -rf "${root}"
  mkdir -p "${condition}"
  local repetition value
  for repetition in 1 2 3; do
    value=$((repetition * 10))
    mkdir -p "${condition}/repeat-${repetition}"
    jq -nc --argjson repetition "${repetition}" --argjson value "${value}" \
      '{ok:true,receiver_mib_per_s:$value,sender_mib_per_s:($value+1),receiver_packets_per_s:($value*100),loss_percent:($value/10),repetition:$repetition}' \
      > "${condition}/repeat-${repetition}/result.json"
  done
  aggregate_condition ygg udp 4 2 "${condition}"
  jq -e '.receiver_mib_per_s | .median == 20 and .min == 10 and .max == 30 and .mean == 20 and .relative_range_percent == 100' \
    "${condition}/condition.json" >/dev/null
  mkdir -p "${root}/baseline/tcp-direct/streams-4"
  cp "${condition}/condition.json" "${root}/baseline/tcp-direct/streams-4/condition.json"
  select_baseline tcp direct "${root}/baseline"
  jq -e '.receiver_mib_per_s.median == 20' "${root}/baseline/tcp-direct/selected.json" >/dev/null
  mkdir -p "${root}/points"
  for value in 1 2; do
    jq -nc --argjson cpu "${value}" --argjson ygg "$((value * 10))" \
      '{ok:true,gomaxprocs:$cpu,direct:{receiver_mib_per_s:{median:40}},ygg:{receiver_mib_per_s:{median:$ygg}}}' \
      > "${root}/points/${value}.json"
  done
  build_scaling_summary "${root}/scaling.json" "${root}/points/1.json" "${root}/points/2.json"
  jq -e '.[0].ygg_speedup_vs_1_cpu == 1 and .[0].scaling_efficiency_percent == 100 and .[1].ygg_speedup_vs_1_cpu == 2 and .[1].scaling_efficiency_percent == 100' \
    "${root}/scaling.json" >/dev/null
  rm -rf "${root}"
  echo '[throughput] statistics self-test passed'
}

validate_config
mkdir -p "${OUT}"
if [ "${1:-}" = '--self-test' ]; then
  self_test
  exit 0
fi

trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

echo '[throughput] waiting for diagnostic nodes'
wait_health node-a
wait_health node-b
get_json node-a '8080/runtime' 10 > "${OUT}/node-a-runtime.json"
get_json node-b '8080/runtime' 10 > "${OUT}/node-b-runtime.json"
ORIGINAL_GOMAXPROCS_A=$(jq -r '.gomaxprocs' "${OUT}/node-a-runtime.json")
ORIGINAL_GOMAXPROCS_B=$(jq -r '.gomaxprocs' "${OUT}/node-b-runtime.json")
NUM_CPU_A=$(jq -r '.num_cpu' "${OUT}/node-a-runtime.json")
NUM_CPU_B=$(jq -r '.num_cpu' "${OUT}/node-b-runtime.json")
AVAILABLE_NUM_CPU=${NUM_CPU_A}
if [ "${NUM_CPU_B}" -lt "${AVAILABLE_NUM_CPU}" ]; then
  AVAILABLE_NUM_CPU=${NUM_CPU_B}
fi

CPU_VALUES=()
for value in "${REQUESTED_CPU_VALUES[@]}"; do
  if [ "${value}" -le "${AVAILABLE_NUM_CPU}" ]; then
    CPU_VALUES+=("${value}")
  fi
done
if [ "${#CPU_VALUES[@]}" -eq 0 ] || [ "${CPU_VALUES[0]}" -ne 1 ]; then
  echo 'no usable CPU scaling values remain after runtime.NumCPU filtering' >&2
  exit 1
fi
MAX_CPU=${CPU_VALUES[$((${#CPU_VALUES[@]} - 1))]}

echo "[throughput] baseline: warm-up=${WARMUP_SECONDS}s measurement=${MEASURE_SECONDS}s repetitions=${REPETITIONS} GOMAXPROCS=${MAX_CPU}"
set_gomaxprocs "${MAX_CPU}" "${OUT}/gomaxprocs/baseline"
for network in tcp udp; do
  for streams in "${STREAMS[@]}"; do
    echo "[throughput] baseline ${network}: streams=${streams}"
    run_condition_pair "${network}" "${streams}" "${MAX_CPU}" "${OUT}/baseline"
  done
  select_baseline "${network}" direct
  select_baseline "${network}" ygg
done

echo '[throughput] CPU scaling with fixed Yggdrasil-selected concurrency'
for gomaxprocs in "${CPU_VALUES[@]}"; do
  echo "[throughput] CPU scaling: GOMAXPROCS=${gomaxprocs}"
  point_root="${OUT}/cpu-scaling/cpu-${gomaxprocs}"
  set_gomaxprocs "${gomaxprocs}" "${point_root}/gomaxprocs"
  for network in tcp udp; do
    streams=$(jq -r '.streams' "${OUT}/baseline/${network}-ygg/selected.json")
    run_condition_pair "${network}" "${streams}" "${gomaxprocs}" "${point_root}"
    build_scaling_point "${network}" "${gomaxprocs}" "${streams}" "${point_root}" "${point_root}/${network}.json"
  done
done

for network in tcp udp; do
  point_files=()
  for gomaxprocs in "${CPU_VALUES[@]}"; do
    point_files+=("${OUT}/cpu-scaling/cpu-${gomaxprocs}/${network}.json")
  done
  build_scaling_summary "${OUT}/cpu-scaling/${network}.json" "${point_files[@]}"
done

echo '[throughput] separate baseline profiles'
set_gomaxprocs "${MAX_CPU}" "${OUT}/profiles/baseline/gomaxprocs"
for network in tcp udp; do
  for transport in direct ygg; do
    streams=$(jq -r '.streams' "${OUT}/baseline/${network}-${transport}/selected.json")
    profile_case "${transport}" "${network}" "${streams}" "${MAX_CPU}" "${OUT}/profiles/baseline/${network}-${transport}"
  done
done

echo '[throughput] separate Yggdrasil CPU-scaling profiles'
for gomaxprocs in "${CPU_VALUES[@]}"; do
  set_gomaxprocs "${gomaxprocs}" "${OUT}/profiles/cpu-${gomaxprocs}/gomaxprocs"
  for network in tcp udp; do
    streams=$(jq -r '.streams' "${OUT}/baseline/${network}-ygg/selected.json")
    profile_case ygg "${network}" "${streams}" "${gomaxprocs}" "${OUT}/profiles/cpu-${gomaxprocs}/${network}-ygg"
  done
done

restore_gomaxprocs
RUN_FINISHED_AT=$(date -u +%Y-%m-%dT%H:%M:%SZ)
RUN_DURATION_SECONDS=$(($(date +%s) - RUN_STARTED_EPOCH))

jq -s '[.[] | .warnings[]?]' "${CONDITION_FILES[@]}" > "${OUT}/warnings.json"
jq -n \
  --slurpfile runtime_a "${OUT}/node-a-runtime.json" --slurpfile runtime_b "${OUT}/node-b-runtime.json" \
  --slurpfile tcp_direct "${OUT}/baseline/tcp-direct/selected.json" --slurpfile tcp_ygg "${OUT}/baseline/tcp-ygg/selected.json" \
  --slurpfile udp_direct "${OUT}/baseline/udp-direct/selected.json" --slurpfile udp_ygg "${OUT}/baseline/udp-ygg/selected.json" \
  --slurpfile tcp_scaling "${OUT}/cpu-scaling/tcp.json" --slurpfile udp_scaling "${OUT}/cpu-scaling/udp.json" \
  --slurpfile warnings "${OUT}/warnings.json" \
  --argjson warmup_seconds "${WARMUP_SECONDS}" --argjson measure_seconds "${MEASURE_SECONDS}" \
  --argjson repetitions "${REPETITIONS}" --argjson profile_seconds "${PROFILE_SECONDS}" \
  --arg streams "${STREAMS[*]}" --arg cpu_values "${CPU_VALUES[*]}" \
  --arg started_at "${RUN_STARTED_AT}" --arg finished_at "${RUN_FINISHED_AT}" \
  --argjson duration_seconds "${RUN_DURATION_SECONDS}" '
  def comparison($direct; $ygg): {
    direct_mib_per_s: $direct.receiver_mib_per_s.median,
    ygg_mib_per_s: $ygg.receiver_mib_per_s.median,
    ygg_percent_of_direct: (if $direct.receiver_mib_per_s.median > 0 then ($ygg.receiver_mib_per_s.median * 100 / $direct.receiver_mib_per_s.median) else 0 end),
    overhead_percent: (if $direct.receiver_mib_per_s.median > 0 then (100 - ($ygg.receiver_mib_per_s.median * 100 / $direct.receiver_mib_per_s.median)) else 0 end)
  };
  ($tcp_direct[0]) as $td | ($tcp_ygg[0]) as $ty | ($udp_direct[0]) as $ud | ($udp_ygg[0]) as $uy |
  {
    ok: ($td.ok and $ty.ok and $ud.ok and $uy.ok and all($tcp_scaling[0][]; .ok) and all($udp_scaling[0][]; .ok)),
    execution: {started_at:$started_at,finished_at:$finished_at,duration_seconds:$duration_seconds},
    methodology: {
      warmup_seconds: $warmup_seconds,
      measure_seconds: $measure_seconds,
      repetitions: $repetitions,
      profile_seconds: $profile_seconds,
      streams: ($streams | split(" ") | map(tonumber)),
      gomaxprocs_values: ($cpu_values | split(" ") | map(tonumber)),
      statistic: "median of clean repetitions; profiles are separate",
      cpu_scope: "GOMAXPROCS for each complete embedded Ratatoskr/Yggdrasil edge process, not dedicated physical cores"
    },
    runtime: {node_a:$runtime_a[0],node_b:$runtime_b[0]},
    baseline: {
      tcp: {direct:$td,ygg:$ty,comparison:comparison($td;$ty)},
      udp: {direct:$ud,ygg:$uy,comparison:comparison($ud;$uy)}
    },
    cpu_scaling: {tcp:$tcp_scaling[0],udp:$udp_scaling[0]},
    warnings: $warnings[0]
  }
' > "${OUT}/summary.json"

CURRENT_STAGE='summary validation'
jq -e '.ok == true' "${OUT}/summary.json" >/dev/null
jq '{ok,methodology,baseline:{tcp:.baseline.tcp.comparison,udp:.baseline.udp.comparison},cpu_scaling:{tcp:[.cpu_scaling.tcp[]|{gomaxprocs,ygg_mib_per_s:.ygg.receiver_mib_per_s.median,ygg_speedup_vs_1_cpu,scaling_efficiency_percent}],udp:[.cpu_scaling.udp[]|{gomaxprocs,ygg_mib_per_s:.ygg.receiver_mib_per_s.median,ygg_speedup_vs_1_cpu,scaling_efficiency_percent}]},warnings}' \
  "${OUT}/summary.json"
