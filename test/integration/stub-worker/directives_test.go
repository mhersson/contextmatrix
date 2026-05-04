package main

import "testing"

func TestParseDirectives(t *testing.T) {
	body := `# Card title

Some prose.

STUB-DIRECTIVE: skip-heartbeat=1
STUB-DIRECTIVE: hang-after-claim
STUB-DIRECTIVE: promote-behaviour=ignore

More prose. STUB-DIRECTIVE: bogus=line should not match because it starts mid-line.
`
	d := parseDirectives(body)
	if !d.skipHeartbeat {
		t.Errorf("skip-heartbeat: want true")
	}
	if !d.hangAfterClaim {
		t.Errorf("hang-after-claim: want true")
	}
	if d.promoteBehaviour != "ignore" {
		t.Errorf("promote-behaviour: got %q want ignore", d.promoteBehaviour)
	}
}

func TestParseDirectivesDefaults(t *testing.T) {
	d := parseDirectives("# empty card body\n")
	if d.skipHeartbeat || d.hangAfterClaim {
		t.Errorf("flags should default false: %+v", d)
	}
	if d.promoteBehaviour != "respect" {
		t.Errorf("promote-behaviour default: got %q want respect", d.promoteBehaviour)
	}
}
