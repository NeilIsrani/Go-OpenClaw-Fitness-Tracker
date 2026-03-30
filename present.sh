#!/bin/bash
PORT=9999
DIR="$(cd "$(dirname "$0")" && pwd)"

# Kill anything already on the port
lsof -ti tcp:$PORT | xargs kill -9 2>/dev/null

echo "Serving at http://localhost:$PORT/architecture.html"
echo "Ctrl+C to stop."

(sleep 0.4 && open "http://localhost:$PORT/architecture.html") &

cd "$DIR"
python3 -m http.server $PORT --bind 127.0.0.1 2>/dev/null
