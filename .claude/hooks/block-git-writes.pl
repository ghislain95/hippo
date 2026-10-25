#!/usr/bin/perl
# Shell reader for block-git-writes.sh: rewrites a Bash command so that only what the shell would
# run is left for the guardrail's checks.
# - Backslash escapes are consumed; a backslash-newline joins the lines (outside comments and
#   quoted heredoc bodies, as the shell does).
# - `#` comments at a word start and heredoc bodies are dropped (a body becomes "Q").
# - Quoted strings ('...', "...", $'...') holding spaces or operators become 'Q', "Q" or "$Q"
#   (when they expand a variable or a substitution): commit messages and PR bodies are never read
#   as flags or commands. Kept: the path after cd/pushd/-C, one-word strings (a one-word option such
#   as "--force" loses its quotes, so quoted flags are still seen).
# - Command substitutions ($(...), `...`, also inside double quotes and unquoted heredocs) become
#   `$` in place, so the word they stand for counts as $-expanded, and their own text is read the
#   same way and emitted after the current line as a `( ... )` subshell block.
# - Arithmetic ($((...)), ((...))) is dropped, so its `<<` is never taken for a heredoc; `((cmd) …)`
#   stays nested subshells, as in the shell. A `case` pattern's `)` becomes `;`.
# - A heredoc that never ends inside $( ... ) makes the reader fail, so the hook blocks git and gh.
use strict;
use warnings;

our $S = do { local $/; <STDIN> };
$S = "" unless defined $S;
pos($S) = 0;
print scan(0);

sub one_word { return $_[0] !~ /[\s;&|(){}`]/; }

# After an opening `((` (pos just past it): arithmetic when its closing parentheses are `))`; the
# shell reads `((cmd) …)` as nested subshells instead. Consumes the arithmetic and returns 1, or
# leaves pos untouched and returns 0.
sub arith {
  my $start = pos($S);
  my $depth = 2;
  while (pos($S) < length($S)) {
    if ($S =~ /\G\(/gc) { $depth++; next; }
    if ($S =~ /\G\)/gc) {
      $depth--;
      next if $depth > 1;
      return 1 if $S =~ /\G\)/gc;
      pos($S) = $start;
      return 0;
    }
    $S =~ /\G./gcs;
  }
  return 1;
}

# Reads a `...` substitution after its opening backquote; returns its text, read.
sub backquoted {
  my $t = "";
  while (pos($S) < length($S)) {
    last if $S =~ /\G`/gc;
    if ($S =~ /\G\\(.)/gcs) { $t .= $1; next; }
    $S =~ /\G(.)/gcs;
    $t .= $1;
  }
  local $S = $t;
  pos($S) = 0;
  return scan(0);
}

# Reads a "..." string after its opening quote; returns its text (substitutions as `$`) and the
# substitutions, read.
sub dquoted {
  my $b = "";
  my @subs;
  while (pos($S) < length($S)) {
    return ($b, \@subs) if $S =~ /\G"/gc;
    if ($S =~ /\G\\\n/gc) { next; }
    if ($S =~ /\G\\(.)/gcs) { $b .= "_"; next; }
    if ($S =~ /\G\$\(\(/gc) {
      if (arith()) { $b .= "0"; next; }
      pos($S) = pos($S) - 1;
      push @subs, scan(1);
      $b .= "\$";
      next;
    }
    if ($S =~ /\G\$\(/gc) { push @subs, scan(1); $b .= "\$"; next; }
    if ($S =~ /\G`/gc) { push @subs, backquoted(); $b .= "\$"; next; }
    $S =~ /\G(.)/gcs;
    $b .= $1;
  }
  return ($b, \@subs);
}

# The substitutions of an unquoted heredoc body (the rest of the body is plain text).
sub body_substitutions {
  my @subs;
  local $S = shift;
  pos($S) = 0;
  while (pos($S) < length($S)) {
    if ($S =~ /\G\\./gcs) { next; }
    if ($S =~ /\G\$\(\(/gc) {
      next if arith();
      pos($S) = pos($S) - 1;
      push @subs, scan(1);
      next;
    }
    if ($S =~ /\G\$\(/gc) { push @subs, scan(1); next; }
    if ($S =~ /\G`/gc) { push @subs, backquoted(); next; }
    $S =~ /\G./gcs;
  }
  return @subs;
}

# Reads commands from pos($S). With $stop, returns at the `)` closing a $( ... ).
sub scan {
  my ($stop) = @_;
  my $o = "";
  my (@pending, @subs, @case);
  my $depth = 0;
  my $word_start = sub { return $o eq "" || $o =~ /[\s;&|(]\z/; };
  my $path_ctx = sub { return $o =~ /(?:^|[\s;&|(])(?:cd|pushd)\s+(?:-[LPe@]+\s+)*\z/ || $o =~ /(?:^|\s)-C\s+\z/; };
  my $flush = sub {
    return unless @subs;
    $o .= "\n" . join("", map { "(\n$_\n)\n" } @subs);
    @subs = ();
  };
  my $quoted = sub {
    my ($q, $b, $expands) = @_;
    if ($path_ctx->() || one_word($b)) {
      return ($b =~ /^-/ && !$path_ctx->()) ? $b : $q . $b . $q;
    }
    return $expands ? "\"\$Q\"" : $q . "Q" . $q;
  };
  while (pos($S) < length($S)) {
    if ($S =~ /\G\n/gc) {
      $flush->();
      $o .= "\n";
      for my $h (@pending) {
        my ($d, $tabs, $is_quoted) = @$h;
        my ($body, $end, $found, $resumed) = ("", "", 0, 0);
        my $line_start = pos($S);
        while (pos($S) < length($S) && $S =~ /\G([^\n]*)(\n|\z)/gc) {
          my ($line, $nl) = ($1, $2);
          my $cmp = $line;
          $cmp =~ s/^\t+// if $tabs;
          if ($cmp eq $d) { $end = $line . $nl; $found = 1; last; }
          # Inside $( ... ), the shell also ends the body on `EOF)`: resume right before the `)`.
          if ($stop && index($cmp, $d . ")") == 0) {
            pos($S) = $line_start + index($line, $d . ")") + length($d);
            $found = $resumed = 1;
            last;
          }
          $body .= $line . $nl;
          $line_start = pos($S);
        }
        die "block-git-writes.pl: heredoc <<$d never ends inside a command substitution\n" if $stop && !$found;
        push @subs, body_substitutions($body) unless $is_quoted;
        $o .= "Q\n" . $end;
        last if $resumed;
      }
      @pending = ();
      $flush->();
      next;
    }
    if ($S =~ /\G\\\n/gc) { $o .= " "; next; }
    if ($S =~ /\G\\(.)/gcs) {
      my $c = $1;
      $o .= ($c =~ /[\x27"`;&|(){}\$#<>]/) ? "_" : "\\" . $c;
      next;
    }
    if ($S =~ /\G\$\x27((?:[^\x27\\]|\\.)*)\x27/gcs) { $o .= $quoted->("\x27", $1, 0); next; }
    if ($S =~ /\G\x27([^\x27]*)\x27/gc) { $o .= $quoted->("\x27", $1, 0); next; }
    if ($S =~ /\G"/gc) {
      my ($b, $inner) = dquoted();
      push @subs, @$inner;
      $o .= $quoted->("\"", $b, $b =~ /\$/);
      next;
    }
    if ($S =~ /\G\$\(\(/gc) {
      if (arith()) { $o .= "0"; next; }
      pos($S) = pos($S) - 1;
      push @subs, scan(1);
      $o .= "\$";
      next;
    }
    if ($word_start->() && $S =~ /\G\(\(/gc) {
      if (arith()) { $o .= "true"; next; }
      pos($S) = pos($S) - 1;
      $depth++;
      $o .= "(";
      next;
    }
    if ($S =~ /\G\$\(/gc) { push @subs, scan(1); $o .= "\$"; next; }
    # `case … in pattern) …;; esac`: a pattern's `)` closes no parenthesis, it starts a command.
    if ($word_start->() && $S =~ /\Gcase(?=\s)/gc) { push @case, $depth; $o .= "case"; next; }
    if ($word_start->() && @case && $S =~ /\Gesac(?=[\s;&|)]|\z)/gc) { pop @case; $o .= "esac"; next; }
    if ($S =~ /\G`/gc) { push @subs, backquoted(); $o .= "\$"; next; }
    if ($word_start->() && $S =~ /\G#[^\n]*/gc) { next; }
    if ($S =~ /\G<<</gc) { $o .= "<<<"; next; }
    if ($S =~ /\G<<(-?)[ \t]*((?:[A-Za-z0-9_.\/-]|\x27[^\x27\n]*\x27|"[^"\n]*"|\\.)+)/gc) {
      my ($tabs, $word) = ($1 eq "-", $2);
      my $is_quoted = $word =~ /[\x27"\\]/ ? 1 : 0;
      (my $d = $word) =~ s/[\x27"\\]//g;
      push @pending, [$d, $tabs, $is_quoted];
      $o .= "<<D";
      next;
    }
    if ($S =~ /\G\(/gc) { $depth++; $o .= "("; next; }
    if ($S =~ /\G\)/gc) {
      if (@case && $depth == $case[-1]) { $o .= ";"; next; }
      if ($stop && $depth == 0) { $flush->(); return $o; }
      $depth-- if $depth > 0;
      $o .= ")";
      next;
    }
    $S =~ /\G(.)/gcs;
    $o .= $1;
  }
  $flush->();
  return $o;
}
