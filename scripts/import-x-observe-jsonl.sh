#!/usr/bin/env bash
set -euo pipefail

TT_STATE_DIR_BASE="${TT_STATE_DIR:-${XDG_STATE_HOME:-$HOME/.local/state}/tt}"
src="${1:-$TT_STATE_DIR_BASE/x-observe.jsonl}"
db="${2:-$TT_STATE_DIR_BASE/x-observe.sqlite}"

die() { printf 'import-x-observe-jsonl.sh: %s\n' "$*" >&2; exit 1; }

command -v sqlite3 >/dev/null 2>&1 || die "sqlite3 is required"
[[ -f $src ]] || die "not a file: $src"
mkdir -p "$(dirname "$db")"

sqlite3 "$db" >/dev/null <<'SQL'
PRAGMA journal_mode=WAL;
CREATE TABLE IF NOT EXISTS x_observe_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ts INTEGER NOT NULL,
  session TEXT NOT NULL,
  status TEXT NOT NULL,
  project TEXT NOT NULL,
  pane_cmd TEXT NOT NULL,
  classifier TEXT NOT NULL,
  unsafe_marker TEXT NOT NULL,
  plain_tail TEXT NOT NULL,
  escaped_tail TEXT NOT NULL,
  prompt_plain TEXT NOT NULL,
  prompt_escaped_visible TEXT NOT NULL,
  stripped_after_prompt TEXT NOT NULL,
  payload_key TEXT NOT NULL UNIQUE
);
CREATE INDEX IF NOT EXISTS x_observe_events_ts_idx ON x_observe_events(ts);
CREATE INDEX IF NOT EXISTS x_observe_events_classifier_idx ON x_observe_events(classifier);
CREATE INDEX IF NOT EXISTS x_observe_events_session_idx ON x_observe_events(session);
SQL

before=$(sqlite3 "$db" 'SELECT count(*) FROM x_observe_events;')
tmp=$(mktemp "${TMPDIR:-/tmp}/x-observe-import.XXXXXX.sql")
trap 'rm -f "$tmp"' EXIT

perl -MJSON::PP -MDigest::SHA=sha1_hex -ne '
  sub sql_quote {
    my ($s) = @_;
    $s = "" unless defined $s;
    $s =~ s/\x27/\x27\x27/g;
    return chr(39) . $s . chr(39);
  }

  chomp;
  next if $_ eq "";
  my $obj = eval { JSON::PP->new->decode($_) };
  if ($@) {
    chomp(my $err = $@);
    die "invalid JSONL at line $.: $err\n";
  }

  my @names = qw(session status project pane_cmd classifier unsafe_marker plain_tail escaped_tail prompt_plain prompt_escaped_visible stripped_after_prompt);
  my %payload = map { $_ => ($obj->{$_} // "") } @names;
  my $key = sha1_hex(JSON::PP->new->ascii->canonical->encode(\%payload));
  my @values = ($obj->{ts} // 0, @payload{@names}, $key);

  print "INSERT OR IGNORE INTO x_observe_events (ts, session, status, project, pane_cmd, classifier, unsafe_marker, plain_tail, escaped_tail, prompt_plain, prompt_escaped_visible, stripped_after_prompt, payload_key) VALUES (";
  print join(",", $values[0] =~ /^\d+$/ ? $values[0] : 0, map { sql_quote($_) } @values[1..12]);
  print ");\n";
' "$src" > "$tmp"

sqlite3 "$db" < "$tmp"
after=$(sqlite3 "$db" 'SELECT count(*) FROM x_observe_events;')

printf 'source: %s\n' "$src"
printf 'database: %s\n' "$db"
printf 'inserted: %s\n' "$((after - before))"
printf 'skipped: %s\n' "$(($(wc -l < "$tmp") - (after - before)))"
printf 'left source unchanged\n'
