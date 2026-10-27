import type { GraphNode } from "../scenarios/types.ts";
import { isReserved, parseCondition } from "./condition.ts";
import { extractProblem, maxText, templateProblem } from "./request-validation.ts";

// What the panels can say about a step before the API does, for every kind that
// is not a request. The same rules as internal/domain/scenario_node.go, said while
// somebody is typing instead of when the save is refused.

export const maxLoop = 1000;
export const maxPause = 300;

const variableName = /^[A-Za-z_][A-Za-z0-9_]*$/;
const secretKey = /^[A-Z][A-Z0-9_]*$/;

// writableName is what a step may call a variable it writes.
export function writableProblem(name: string | undefined): string | undefined {
  if (!variableName.test(name ?? "")) return "A variable name uses letters, digits and _, and does not start with a digit.";
  if (isReserved(name as string)) return `${name} reads the last response, so no step may write it.`;
  return undefined;
}

// expressionProblem says what is wrong with a condition or an assertion, and where.
export function expressionProblem(expression: string | undefined): string | undefined {
  const parsed = parseCondition(expression ?? "");
  if (parsed.ok) return undefined;
  return `Character ${parsed.problem.offset + 1}: ${parsed.problem.message}`;
}

// loopProblem: a maximum is always asked for, and a list always comes with the
// name its elements go by.
export function loopProblem(node: GraphNode): string | undefined {
  const times = node.iterations ?? 0;
  if (!Number.isInteger(times) || times < 1 || times > maxLoop) {
    return `Give the loop a maximum between 1 and ${maxLoop}.`;
  }

  const over = node.over ?? "";
  const item = node.item ?? "";
  if (over === "" && item === "") return undefined;

  if (over === "") return "Choose the list this loop takes its elements from.";
  if (item === "") return "Name the variable that holds each element.";
  if (!variableName.test(over)) return `${over} is not a variable name.`;
  const written = writableProblem(item);
  if (written) return written;
  if (over === item) return "The element cannot have the same name as the list.";
  return undefined;
}

// pauseProblem: a time between 0 and 5 minutes, or a range inside that.
export function pauseProblem(node: GraphNode): string | undefined {
  const min = node.seconds ?? 0;
  const max = node.seconds_max ?? 0;

  if (!(min > 0) || min > maxPause) return `Wait more than 0 and at most ${maxPause} seconds.`;
  if (max !== 0 && (max < min || max > maxPause)) {
    return `The longest wait is between the shortest and ${maxPause} seconds.`;
  }
  return undefined;
}

// setVariableProblem: a name, then either a value or a secret's key, never both.
export function setVariableProblem(node: GraphNode): string | undefined {
  const written = writableProblem(node.variable);
  if (written) return written;

  const key = node.from_env ?? "";
  if (key === "") {
    if ((node.value ?? "").length > maxText) return `The value is longer than ${maxText} characters.`;
    return templateProblem(node.value ?? "");
  }

  if (node.value) return "A variable takes a value or a secret, not both.";
  if (key.length > 128) return "The key is longer than 128 characters.";
  if (!secretKey.test(key)) return "A secret key uses upper case letters, digits and _, and starts with a letter.";
  if (key.startsWith("K6_")) return "K6_ names configure k6 itself and cannot be read as a secret.";
  if (key === "BASE_URL") return "BASE_URL is the target the environment points at, and is not a secret.";
  return undefined;
}

// nodeProblem is the first thing wrong with any step that is not a request, in a
// sentence, or nothing. Requests have their own, since they have many fields.
export function nodeProblem(node: GraphNode): string | undefined {
  switch (node.kind) {
    case "extract_variable":
      return extractProblem(node);
    case "condition":
    case "assertion":
      return expressionProblem(node.expression);
    case "loop":
      return loopProblem(node);
    case "think_time":
      return pauseProblem(node);
    case "set_variable":
      return setVariableProblem(node);
    default:
      return undefined;
  }
}
