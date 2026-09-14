#!/usr/bin/env bash
set -euo pipefail

TARGET=89.0
ENFORCE=false
RUN_TESTS=true

while [[ $# -gt 0 ]]; do
  case "$1" in
    --enforce)
      ENFORCE=true
      shift
      ;;
    --threshold)
      TARGET="$2"
      shift 2
      ;;
    --no-test)
      RUN_TESTS=false
      shift
      ;;
    *)
      echo "Unknown argument: $1" >&2
      exit 1
      ;;
  esac
done

if [[ "$RUN_TESTS" == "true" ]]; then
  echo "==> Running tests with coverage profiling..."
  go test -covermode=atomic -coverprofile=coverage.out ./... > /dev/null
fi

if [[ ! -f coverage.out ]]; then
  echo "Error: coverage.out not found" >&2
  exit 1
fi

python3 -c "
import sys

cov_file = 'coverage.out'
target_pct = float('$TARGET')
enforce = ('$ENFORCE' == 'true')

total_stmts = 0
covered_stmts = 0
by_file = {}

with open(cov_file) as f:
    for line in f:
        line = line.strip()
        if not line or line.startswith('mode:'):
            continue
        parts = line.split(' ')
        loc = parts[0]
        stmts = int(parts[1])
        count = int(parts[2])
        file_path = loc.split(':')[0]

        # Exclude generated templ files
        if '_templ.go' in file_path:
            continue

        total_stmts += stmts
        if count > 0:
            covered_stmts += stmts

        if file_path not in by_file:
            by_file[file_path] = [0, 0]
        by_file[file_path][1] += stmts
        if count > 0:
            by_file[file_path][0] += stmts

pct = (covered_stmts / total_stmts * 100.0) if total_stmts > 0 else 0.0
needed = max(0, int((target_pct / 100.0 * total_stmts) - covered_stmts + 0.999))

print('=' * 65)
print('  HANDWRITTEN GO TEST COVERAGE (EXCLUDING *_templ.go)')
print('=' * 65)
print(f'  Covered Statements : {covered_stmts:,} / {total_stmts:,}')
print(f'  Handwritten Coverage: {pct:6.2f}%')
print(f'  Target Threshold   : {target_pct:6.2f}%')

if pct >= target_pct:
    print(f'  Status             : \033[32mPASSED (+{pct - target_pct:.2f}% above target)\033[0m')
else:
    print(f'  Status             : \033[33mIN PROGRESS (-{target_pct - pct:.2f}%, {needed} stmts needed)\033[0m')
print('=' * 65)

# Print top 10 deficit files
uncovered_files = sorted(
    [f for f in by_file.items() if f[1][1] > f[1][0]],
    key=lambda x: (x[1][1] - x[1][0]),
    reverse=True
)

if uncovered_files and pct < target_pct:
    print('\nTop 10 Uncovered Files:')
    for f, (cov, tot) in uncovered_files[:10]:
        fpct = cov / tot * 100.0 if tot > 0 else 0.0
        print(f'  {f:<50} {cov:4d}/{tot:<4d} ({fpct:5.1f}%) [gap: {tot-cov}]')
    print()

if enforce and pct < target_pct:
    sys.exit(1)
"
