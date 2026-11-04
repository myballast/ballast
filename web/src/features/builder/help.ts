import type { NodeKind } from "../scenarios/types.ts";

// What the builder explains to somebody who has never drawn a load test, in one
// place: the words of every "i". The names of k6 stay in the sentences, so that they
// are learnt, but nothing is left to be guessed.

export const kindHelp: Record<NodeKind, string> = {
  http_request:
    "Calls one endpoint of the target, the way a browser or an app would. This is where the scenario measures something.",
  extract_variable:
    "Keeps a value from the last response (a field of the body, a header or the status) in a variable, so that a later step can use it, such as a login token.",
  set_variable:
    "Gives a variable a fixed value, or the value of a secret of the environment (a password, an API key) that Ballast never shows.",
  condition: "Takes one way or the other depending on a test, such as “the status is 200”. It has two exits: true and false.",
  loop: "Repeats a few steps: a set number of times, or once for each element of a list. It always has a maximum, so a run cannot go on for ever.",
  think_time: "Waits a moment, like a person reading a page, so that the load looks like people and not a machine gun.",
  assertion:
    "Checks something about the last response. It can only note the failure, or stop the scenario at that point.",
  end: "Where a path of the scenario stops. Every path has to lead to an End.",
};

export const fieldHelp = {
  label: "A name for people. It shows on the canvas and is written in the script as a comment; it changes nothing else.",
  request:
    "The method and the path, after the address of the environment you run against. A name between braces, as in /users/{id}, is a variable, and the path itself is the request's name in the results.",
  headers:
    "Extra lines sent with the request, such as Content-Type. A credential (Authorization, Cookie) is never typed: it reads a variable, which a secret of the environment fills when the run starts.",
  query: "The parameters after the ? in the address, one name and one value a row. {{name}} in a value reads a variable.",
  body: "What the request sends, usually JSON. {{name}} reads a variable; a single brace is just text. Picking an endpoint of the catalog fills it in from the example in your file.",
  critical: "When this request fails (an error status or no answer), the scenario stops there for that user instead of carrying on with the next step.",
  extractSource:
    "Where in the last response to look: in the JSON body, in a header, or at the status code. The response is the one of the request just before this step.",
  extractFrom:
    "For the body, the path of the field, such as data.token. For a header, its name. If it is missing or empty the iteration stops and the run says which variable it could not read.",
  extractVariable:
    "The name the value is kept under. Later steps read it as {{name}} in a path, a header or a body.",
  setVariable: "The name the value is kept under. Later steps read it as {{name}}.",
  setFrom:
    "A fixed value you type, or the key of a secret stored on the environment. A secret is read when the run starts and is never written in the script or shown.",
  condition:
    "What to test, using variables you have and the last response (response_status, response_time_ms). For example: response_status == 200, or token != \"\". It is not code: only comparisons joined by && or ||.",
  assertion:
    "What must be true after the last request, in the same words as a condition. It is recorded among the run's checks.",
  assertionFail: "Count only keeps going and records the miss. Stop ends the iteration at this step.",
  loopList: "A variable that holds a list, for example one kept by an Extract step. The loop goes through it once.",
  loopItem: "The variable that takes each element of the list in turn, so the steps in the loop can use it.",
  loopMax:
    "The most times the loop may run. It is required because a list from somebody else's server has no length you can promise, and this is what bounds the run.",
  wait: "How long each virtual user pauses. A range draws a different time each pass, which is closer to people.",
} as const;

// The bar above the canvas, and the canvas.
export const barHelp = {
  target:
    "The environment the run is aimed at. Only verified environments are listed: Ballast loads nothing it has not been shown to own.",
  load: "The profile a run starts from: how many virtual users and for how long. Change it in the Load profile tab below the canvas.",
  saved: "Saving creates a version of the scenario. A run always executes the saved version, so unsaved changes are not part of it.",
  start: "The scenario starts here. Every iteration begins at this step and follows the wires.",
  variableWire: "A step that writes a variable names it on the wire that leaves it, so you can follow where a value goes.",
} as const;

export const howItWorks = [
  { title: "Add steps", text: "Use Nodes to open the list, then drag a kind onto the canvas or click it. The first step is a request." },
  {
    title: "Wire them",
    text: "Drag from the dot on the right of a step to the step that comes next. A condition has two exits (true and false), a loop has body and after.",
  },
  {
    title: "Fill each step in",
    text: "Click a step to open its fields. {{name}} reads a variable. A wire labelled var token carries the value that step wrote.",
  },
  {
    title: "Validate, save, run",
    text: "Validate lists every fault on its step. Save makes a version. Run executes the saved version on the target you picked.",
  },
] as const;

export const shortcuts = [
  { keys: "Ctrl/Cmd + C, V", does: "Copy the selected steps and paste them, here or in another scenario" },
  { keys: "Ctrl/Cmd + D", does: "Duplicate the selected steps" },
  { keys: "Ctrl/Cmd + Z, Y", does: "Undo and redo" },
  { keys: "Delete", does: "Remove the selected steps" },
  { keys: "Shift + drag", does: "Select several steps with a frame" },
  { keys: "Ctrl/Cmd + B", does: "Collapse the sidebar for more room" },
] as const;
