# tests/ui — the browser suite, as one line in run.sh's summary. The
# real assertions and their output are Playwright's.

test_panel_in_a_browser() {
  if [ ! -d "$TESTS_DIR/ui/node_modules" ]; then
    skip "tests/ui has no node_modules; run npm install there"
    return
  fi
  if (cd "$TESTS_DIR/ui" && npx playwright test); then
    pass "the panel's browser suite (tests/ui)"
  else
    fail "the panel's browser suite (tests/ui); see its output above"
  fi
}
