export type LogLevel = "error" | "warn";

// What a line of k6's output says about itself. k6 writes its own messages as
//
//	time="2026-10-16T14:06:06Z" level=error msg="..."
//	ERRO[0006] ...    WARN[0004] ...
//
// and a script's console output has whatever its author wrote, which by habit
// starts with the level: "ERROR POST /api/checkout 503". All of them go to
// stderr, so the stream says nothing about the level; the text does.
//
// Deliberately conservative: a level is read from where one is written — the
// start of the line, or a level= field — and not from the word wherever it
// appears, since "0 errors" and "authorization error rate" are not errors.
const error = /^\s*(?:\x1b\[[0-9;]*m)?(?:ERROR|ERRO|FATAL|FATA)\b|\blevel=(?:error|fatal)\b/i;
const warn = /^\s*(?:\x1b\[[0-9;]*m)?(?:WARNING|WARN)\b|\blevel=warn(?:ing)?\b/i;

export function levelOf(message: string): LogLevel | null {
  if (error.test(message)) return "error";
  if (warn.test(message)) return "warn";
  return null;
}

// The level written at the start of a line, and what follows it, so that a line
// drawn with its level in a column of its own does not say it twice. A level
// written inside a line (level=error) is part of what k6 wrote and stays.
const leading = /^\s*(?:\x1b\[[0-9;]*m)?(?:ERROR|ERRO|FATAL|FATA|WARNING|WARN)\b(?:\[\d+\])?\s*/i;

export function withoutLevel(message: string): string {
  const rest = message.replace(leading, "");
  // A line that is nothing but its level keeps it: an empty row says nothing.
  return rest === "" ? message : rest;
}
