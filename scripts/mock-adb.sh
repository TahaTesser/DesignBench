#!/usr/bin/env bash
set -euo pipefail

usage() {
	echo "mock-adb: unsupported invocation: $*" >&2
	exit 1
}

mock_dumpsys() {
	local section="$1"
	shift || true
	case "$section" in
		meminfo)
			cat <<'EOF'
Applications Memory Usage (kB):
Uptime: 123456 Realtime: 123456

** MEMINFO in pid 4242 [com.example.app] **
                    Pss  Private
           Dalvik  1024   512
            TOTAL  4096   2048
EOF
			;;
		cpuinfo)
			echo "Load: 5.00 / 3.00 / 2.00"
			echo " 10% 4242/com.example.app"
			;;
		*)
			echo ""
			;;
	esac
}

mock_getprop() {
	case "${1:-}" in
		ro.product.model)
			echo "Pixel Mock"
			;;
		ro.build.version.release)
			echo "14"
			;;
		*)
			echo ""
			;;
	esac
}

handle_shell() {
	if [[ $# -eq 0 ]]; then
		usage "shell"
	fi
	local sub="$1"
	shift || true
	case "$sub" in
		am)
			echo "Starting: Intent { act=android.intent.action.MAIN cmp=mock/.BenchmarkActivity }"
			echo "Status: ok"
			echo "LaunchState: COLD"
			echo "ThisTime: 8"
			echo "TotalTime: 12"
			echo "WaitTime: 14"
			echo "Complete"
			;;
		dumpsys)
			mock_dumpsys "$@"
			;;
		pidof)
			echo "4242"
			;;
		top)
			echo "Tasks: 1 total"
			echo "PID   CPU%   NAME"
			echo "4242  10%   com.example.app"
			;;
		ps)
			echo "PID   NAME"
			echo "4242  com.example.app"
			;;
		getprop)
			mock_getprop "$@"
			;;
		wm)
			if [[ "${1:-}" == "size" ]]; then
				echo "Physical size: 1080x2400"
				return
			fi
			usage "wm $*"
			;;
		cat)
			if [[ "${1:-}" =~ ^/proc/([0-9]+)/stat$ ]]; then
				echo "4242 (mock) S 0 0 0 0 0 0 0 0 0 0 100 50 0 0 0 0 0 0 0 0 0 0 0 0"
				return
			fi
			return 0
			;;
		*)
			usage "shell $sub $*"
			;;
	esac
}

DEVICE_ID="mock-device"
if [[ "${1:-}" == "-s" ]]; then
	DEVICE_ID="$2"
	shift 2
fi

if [[ $# -eq 0 ]]; then
	usage "(missing command)"
fi

cmd="$1"
shift || true

case "$cmd" in
	devices)
		if [[ "${1:-}" == "-l" ]]; then
			echo "List of devices attached"
			echo "${DEVICE_ID}\tdevice usb:1-1 product:mock model:Pixel_Mock device:pixelmock"
			exit 0
		fi
		usage "devices $*"
		;;
	shell)
		handle_shell "$@"
		;;
	getprop)
		echo ""
		;;
	*)
		echo "mock-adb: noop for $cmd $*" >&2
		;;
esac

exit 0
