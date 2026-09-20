#!/bin/bash

# Claude Code status line — Starship-inspired with icons
# Receives JSON on stdin from Claude Code

input=$(cat)

# Extract JSON data
cwd=$(echo "$input" | jq -r '.workspace.current_dir // .cwd // "~"')
model=$(echo "$input" | jq -r '.model.display_name // .model.id // "Claude"')
pct=$(echo "$input" | jq -r '.context_window.used_percentage // 0' | cut -d. -f1)
used_k=$(echo "$input" | jq -r '((.context_window.current_usage | .input_tokens + .cache_creation_input_tokens + .cache_read_input_tokens + .output_tokens) // 0) / 1000 | floor')
max_k=$(echo "$input" | jq -r '(.context_window.context_window_size // 0) / 1000 | floor')

# Use configured autoCompactWindow as the effective window when set, so the
# displayed window matches the engine's auto-compact trigger (not the raw model window)
acw=$(jq -r '.autoCompactWindow // empty' ~/.claude/settings.json 2>/dev/null)
if [ -n "$acw" ] && [ "$acw" -gt 0 ] 2>/dev/null; then
  max_k=$((acw / 1000))
fi

# Format directory (~ for home)
if [[ "$cwd" == "$HOME"* ]]; then
  display_dir="${cwd/#$HOME/~}"
else
  display_dir="$cwd"
fi

# Git branch + worktree
git_info=""
if git -C "$cwd" rev-parse --git-dir >/dev/null 2>&1; then
  branch=$(git -C "$cwd" symbolic-ref --short HEAD 2>/dev/null || git -C "$cwd" rev-parse --short HEAD 2>/dev/null)
  if [ -n "$branch" ]; then
    dirty=""
    if ! git -C "$cwd" diff-index --quiet HEAD -- 2>/dev/null; then
      dirty="*"
    fi
    # Check if in a worktree (git dir is a file pointing to main repo)
    wt=""
    git_dir=$(git -C "$cwd" rev-parse --git-dir 2>/dev/null)
    if [ -f "$git_dir" ] || echo "$input" | jq -e '.worktree' >/dev/null 2>&1; then
      wt_name=$(echo "$input" | jq -r '.worktree.name // empty' 2>/dev/null)
      [ -z "$wt_name" ] && wt_name=$(basename "$cwd")
      wt=" 🌳 ${wt_name}"
    fi
    git_info=" on  ${branch}${dirty}${wt}"
  fi
fi

# Auto-compact: key only exists in ~/.claude.json when disabled (false)
# Absent = enabled (default), false = disabled
# When enabled, triggers at ~80% so effective max is 80% of context window
compact_val=$(jq -r 'if .autoCompactEnabled == false then "false" else "true" end' ~/.claude.json 2>/dev/null)
compact_pct=${CLAUDE_AUTOCOMPACT_PCT_OVERRIDE:-80}
if [ "$compact_val" = "false" ]; then
  compact_icon="❌ auto-compact"
else
  compact_icon="🔄 auto-compact"
  # autoCompactWindow (if set) is already the effective auto-compact max — use it
  # directly so the bar hits 100% exactly where the engine compacts. Otherwise
  # fall back to compact_pct% of the model context window.
  if [ -z "$acw" ] || ! [ "$acw" -gt 0 ] 2>/dev/null; then
    max_k=$((max_k * compact_pct / 100))
  fi
  # Recalculate pct against effective max
  if [ "$max_k" -gt 0 ]; then
    pct=$((used_k * 100 / max_k))
  fi
fi

# Time
now=$(date '+%H:%M')

# Build one-line version, split to two if too long
line1=$(printf '🕐 %s 📂 %s%s • 🤖 %s' "$now" "$display_dir" "$git_info" "$model")
line2=$(printf '📊 %s%% (%sk/%sk) • %s' "$pct" "$used_k" "$max_k" "$compact_icon")
oneline="$line1 • $line2"

# Get terminal width (fallback 80)
cols=${COLUMNS:-$(tput cols 2>/dev/null || echo 80)}

if [ ${#oneline} -le "$cols" ]; then
  printf '%s' "$oneline"
else
  printf '%s\n%s' "$line1" "$line2"
fi
