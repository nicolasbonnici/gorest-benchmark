#!/bin/bash
# Install git hooks for the GoREST plugin

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
HOOKS_DIR="$PROJECT_ROOT/.git/hooks"

echo "📦 Installing git hooks..."
echo ""

# Check if .git directory exists
if [ ! -d "$PROJECT_ROOT/.git" ]; then
    echo "❌ Error: .git directory not found. Are you in a git repository?"
    exit 1
fi

# Install pre-commit hook
if [ -f "$SCRIPT_DIR/pre-commit" ]; then
    cp "$SCRIPT_DIR/pre-commit" "$HOOKS_DIR/pre-commit"
    chmod +x "$HOOKS_DIR/pre-commit"
    echo "✅ Installed pre-commit hook"
else
    echo "⚠️  Warning: .githooks/pre-commit not found"
fi

echo ""
echo "📦 Git hooks installation completed!"
echo ""
echo "The following hooks are now active:"
echo "  • pre-commit: Runs 'make lint && make test' before each commit"
echo ""
echo "To skip hooks for a specific commit, use: git commit --no-verify"
