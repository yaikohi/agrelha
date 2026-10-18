set -eu
MODS=/config-mods/mods.txt
DEST=/config/bepinex/plugins/managed
CACHE=/config/bepinex/.cache

rm -rf "$DEST"
mkdir -p "$DEST" "$CACHE"

if [ ! -s "$MODS" ] || ! grep -qvE '^[[:space:]]*(#|$)' "$MODS"; then
  echo ">> no mods declared - running clean BepInEx"
  chown -R 1000:1000 /config/bepinex 2>/dev/null || true
  exit 0
fi

apk add --no-cache curl unzip jq >/dev/null 2>&1

# Thunderstore being down and a mod being deleted are different
# problems. curl -f collapses both into exit 22, so ask for the
# status: 404/410 is permanent and reported at once, 5xx and
# connection failures are retried. Kubernetes retries a failed
# init container too, but its backoff grows to five minutes -
# far too blunt for a momentary blip.
ATTEMPTS=3
RETRY_SLEEP=3

http_get() { # url outfile -> prints status
  # curl already prints 000 for a connection failure, so only
  # substitute when it printed nothing at all.
  code=$(curl -sSL -o "$2" -w '%{http_code}' "$1" 2>/dev/null) || true
  [ -n "$code" ] || code=000
  printf '%s' "$code"
}

fetch_file() { # url outfile
  url=$1; out=$2; n=1
  while :; do
    code=$(http_get "$url" "$out")
    case "$code" in
      2??) return 0 ;;
      404|410)
        echo "ERROR: $url no longer exists on Thunderstore (HTTP $code)" >&2
        return 1 ;;
    esac
    if [ "$n" -ge "$ATTEMPTS" ]; then
      echo "ERROR: $url failed after $n attempts (last HTTP $code)" >&2
      return 1
    fi
    echo ">> [retry $n/$ATTEMPTS] HTTP $code from $url"
    n=$((n+1)); sleep "$RETRY_SLEEP"
  done
}

fetch_latest() { # ns name -> prints version
  body=$(mktemp)
  if ! fetch_file "https://thunderstore.io/api/experimental/package/$1/$2/" "$body"; then
    rm -f "$body"; return 1
  fi
  ver=$(jq -re '.latest.version_number' < "$body" 2>/dev/null) || {
    echo "ERROR: $1/$2 returned no version" >&2; rm -f "$body"; return 1
  }
  rm -f "$body"; printf '%s' "$ver"
}

ACTIVE=$(mktemp)
grep -vE '^[[:space:]]*(#|$)' "$MODS" | tr -d '\r' | sed 's/[[:space:]]*$//; s/?$//' | while read -r entry; do
  [ -n "$entry" ] || continue
  case "$entry" in
    */*/*) ns=${entry%%/*}; rest=${entry#*/}; name=${rest%%/*}; ver=${rest#*/} ;;
    *-*)
      # Thunderstore's canonical id is Namespace-Name-Version, which
      # is what its own copy button gives you. Package names never
      # contain hyphens (they use underscores), so a trailing
      # semver segment is the version and not part of the name.
      ns=${entry%%-*}; rest=${entry#*-}
      case "$rest" in
        *-[0-9]*.[0-9]*.[0-9]*) name=${rest%-*}; ver=${rest##*-} ;;
        *)                      name=$rest;      ver="" ;;
      esac
      ;;
    *)     echo "ERROR: cannot parse mod entry '$entry'" >&2; exit 1 ;;
  esac

  if [ -z "$ver" ]; then
    ver=$(fetch_latest "$ns" "$name") || exit 1
    echo ">> [resolved] $ns/$name -> $ver"
  fi

  zip="$CACHE/$ns-$name-$ver.zip"
  echo "$ns-$name-$ver.zip" >> "$ACTIVE"

  if [ ! -f "$zip" ]; then
    echo ">> [downloading] $ns/$name/$ver"
    tmpzip=$(mktemp)
    if fetch_file "https://thunderstore.io/package/download/$ns/$name/$ver/" "$tmpzip"; then
      mv "$tmpzip" "$zip"
    else
      rm -f "$tmpzip"
      exit 1
    fi
  else
    echo ">> [cached] $ns/$name/$ver"
  fi

  tmp=$(mktemp -d)
  # unzip exits 1 for warnings, not just errors - a mod packed on
  # Windows warns about backslash separators and would otherwise
  # take the whole server down under set -e. Only 2+ is fatal.
  rc=0
  unzip -oq "$zip" -d "$tmp" || rc=$?
  if [ "$rc" -gt 1 ]; then
    echo "ERROR: cannot unpack $ns/$name/$ver (unzip exit $rc)" >&2
    exit 1
  fi
  mkdir -p "$DEST/$name"
  find "$tmp" -iname '*.dll' -exec cp {} "$DEST/$name/" \;
  rm -rf "$tmp"
done

for f in "$CACHE"/*.zip; do
  [ -e "$f" ] || continue
  if ! grep -qxF "$(basename "$f")" "$ACTIVE" 2>/dev/null; then
    echo ">> [pruning cache] $(basename "$f")"
    rm -f "$f"
  fi
done
rm -f "$ACTIVE"

echo ">> installed $(find "$DEST" -iname '*.dll' | wc -l) plugin dll(s)"
chown -R 1000:1000 /config/bepinex 2>/dev/null || true
