#!/usr/bin/env bash
set -euo pipefail

REPO="ringclaw/ringclaw"
WIKI_DIR=$(mktemp -d)
DOCS_DIR="docs"
SITE_URL="https://ringclaw.github.io/ringclaw"
FOOTER='> This page is auto-synced from [VitePress docs]('"$SITE_URL"'). Edit the source there.'

# Clone wiki repo
git clone "https://x-access-token:${GITHUB_TOKEN}@github.com/${REPO}.wiki.git" "$WIKI_DIR"

# Clear old wiki content (keep .git)
find "$WIKI_DIR" -maxdepth 1 -not -name '.git' -not -path "$WIKI_DIR" -exec rm -rf {} +

# Convert a VitePress markdown file to plain GitHub Wiki markdown:
#   - Strip YAML frontmatter
#   - Strip <script> blocks
#   - Convert ::: containers to blockquotes
convert_md() {
    local src="$1"
    local dst="$2"
    local in_front=0 front_count=0
    local in_script=0
    local in_container=0

    > "$dst"
    while IFS= read -r line || [[ -n "$line" ]]; do
        # Strip YAML frontmatter (between first pair of ---)
        if [[ "$line" == "---" && $front_count -lt 2 ]]; then
            in_front=$((1 - in_front))
            front_count=$((front_count + 1))
            continue
        fi
        [[ $in_front -eq 1 ]] && continue

        # Strip <script> blocks
        if [[ "$line" == "<script"* ]]; then in_script=1; continue; fi
        if [[ "$line" == "</script>"* ]]; then in_script=0; continue; fi
        [[ $in_script -eq 1 ]] && continue

        # Skip VitePress hero containers entirely
        if [[ "$line" =~ ^:::+[[:space:]]hero ]]; then in_container=2; continue; fi

        # Convert ::: tip/warning/danger/info containers to blockquotes
        if [[ "$line" =~ ^:::+[[:space:]](tip|warning|danger|info)(.*) ]]; then
            local ctype="${BASH_REMATCH[1]}"
            local ctitle="${BASH_REMATCH[2]}"
            ctype="$(echo "${ctype:0:1}" | tr '[:lower:]' '[:upper:]')${ctype:1}"
            ctitle="${ctitle# }"
            if [[ -n "$ctitle" ]]; then
                echo "> **${ctype}: ${ctitle}**" >> "$dst"
            else
                echo "> **${ctype}**" >> "$dst"
            fi
            in_container=1
            continue
        fi

        # End of container
        if [[ "$line" =~ ^:::+$ ]]; then
            if [[ $in_container -eq 2 ]]; then in_container=0; continue; fi
            if [[ $in_container -eq 1 ]]; then echo "" >> "$dst"; in_container=0; continue; fi
            continue
        fi

        [[ $in_container -eq 2 ]] && continue
        if [[ $in_container -eq 1 ]]; then echo "> $line" >> "$dst"; continue; fi

        echo "$line" >> "$dst"
    done < "$src"

    printf '\n---\n%s\n' "$FOOTER" >> "$dst"
}

# Map a docs path to a Wiki page name
# docs/guide/commands.md -> Guide-Commands.md
# docs/zh/guide/commands.md -> ZH-Guide-Commands.md
map_name() {
    local path="$1"
    # Remove docs/ prefix
    path="${path#docs/}"
    # Remove .md extension
    path="${path%.md}"
    # Replace index with parent dir name
    if [[ "$path" == */index ]]; then
        path="${path%/index}"
    fi
    # Replace / with -
    path="${path//\//-}"
    # Title-case each segment
    local result=""
    IFS='-' read -ra parts <<< "$path"
    for part in "${parts[@]}"; do
        result="${result}$(echo "${part:0:1}" | tr '[:lower:]' '[:upper:]')${part:1}-"
    done
    # Remove trailing -
    result="${result%-}"
    echo "${result}.md"
}

# Copy README files
cp README.md "$WIKI_DIR/Home.md"
printf '\n---\n%s\n' "$FOOTER" >> "$WIKI_DIR/Home.md"

if [ -f README_CN.md ]; then
    cp README_CN.md "$WIKI_DIR/Home-CN.md"
    printf '\n---\n%s\n' "$FOOTER" >> "$WIKI_DIR/Home-CN.md"
fi

# Process docs
sidebar_en=""
sidebar_zh=""

while IFS= read -r file; do
    # Skip VitePress landing pages (contain Vue components)
    basename=$(basename "$file")
    if [[ "$basename" == "index.md" && "$file" == "docs/index.md" ]]; then
        continue
    fi
    if [[ "$basename" == "index.md" && "$file" == "docs/zh/index.md" ]]; then
        continue
    fi

    wiki_name=$(map_name "$file")
    convert_md "$file" "$WIKI_DIR/$wiki_name"

    # Extract title from first # heading
    title=$(grep -m1 '^# ' "$file" | sed 's/^# //' || echo "$wiki_name")
    link="[[${title}|${wiki_name%.md}]]"

    if [[ "$file" == docs/zh/* ]]; then
        sidebar_zh="${sidebar_zh}  - ${link}\n"
    else
        sidebar_en="${sidebar_en}  - ${link}\n"
    fi
done < <(find "$DOCS_DIR" -name '*.md' -not -path '*/node_modules/*' -not -path '*/.vitepress/*' | sort)

# Generate sidebar
cat > "$WIKI_DIR/_Sidebar.md" << EOF
**RingClaw**

- [[Home]]
- [[Home CN|Home-CN]]

**English**
$(printf '%b' "$sidebar_en")
**中文**
$(printf '%b' "$sidebar_zh")
EOF

# Commit and push
cd "$WIKI_DIR"
git add -A
if git diff --cached --quiet; then
    echo "Wiki is up to date, nothing to commit."
else
    git config user.name "github-actions[bot]"
    git config user.email "41898282+github-actions[bot]@users.noreply.github.com"
    git commit -m "sync: update wiki from docs"
    git push
    echo "Wiki updated successfully."
fi

# Cleanup
rm -rf "$WIKI_DIR"
