"use client";

import "@xyflow/react/dist/style.css";

import {
  Background,
  BackgroundVariant,
  MiniMap,
  Panel,
  ReactFlow,
  ReactFlowProvider,
  applyNodeChanges,
  useNodesInitialized,
  useReactFlow,
  useStore,
  type Connection,
  type Edge,
  type EdgeChange,
  type NodeChange,
} from "@xyflow/react";
import { Play, Redo2, Undo2 } from "lucide-react";
import Link from "next/link";
import { useCallback, useEffect, useId, useMemo, useRef, useState, useTransition } from "react";

import { Time } from "@/components/time";
import { Button } from "@/components/ui/button";
import type { CatalogEndpoint } from "@/features/endpoints/types";
import { launchDefaultRun } from "@/features/runs/actions";
import { profileSummary, runBlockedReason } from "@/features/scenarios/workspace";
import { useUnsavedGuard } from "@/features/scenarios/use-unsaved-guard";
import type { GraphEdge, GraphNode, NodeKind, Scenario, ScenarioGraph } from "@/features/scenarios/types";

import { saveGraph, validateGraph } from "../actions";
import { fromEndpoint } from "../catalog-node";
import { hasProblems, isCredentialHeader, requestProblems, summaryOfProblems } from "../request-validation";
import { bindSecretBefore, unboundSecrets, variablesBefore } from "../variables";
import {
  canonical,
  columnWidth,
  connect,
  edgeId,
  freeExit,
  issues,
  layout,
  nextId,
  rowHeight,
  without,
  type Position,
} from "../graph-model";
import { emptyHistory, redo, remember, undo, type History } from "../history";
import { describe, infoOf, newNode } from "../kinds";
import { EmptyState } from "./empty-state";
import { Inspector } from "./inspector";
import { Palette, dragType } from "./palette";
import { StepNode, type StepData, type StepFlowNode } from "./step-node";

const nodeTypes = { step: StepNode };
const fitOptions = { maxZoom: 1, padding: 0.2 };

type Target = { id: string; name: string };

// What the canvas keeps about a node beside the graph: where it is, whether it is
// selected, and what React Flow measured. None of it is saved.
type Box = { position: Position; selected?: boolean; dragging?: boolean; measured?: { width?: number; height?: number } };

type Snapshot = { graph: ScenarioGraph; places: Record<string, Position> };

type Message = { kind: "ok" | "error"; text: string };

export type BuilderProps = {
  projectId: string;
  projectName: string;
  scenario: Scenario & { graph: ScenarioGraph };
  targets: Target[];
  canEdit: boolean;
  canRun: boolean;
  now: Date;
  // The project's catalog, for somebody who may write scenarios; undefined for
  // anyone else.
  endpoints?: CatalogEndpoint[];
  // The keys — never the values — of each verified environment's secrets, by
  // environment id. Present only for somebody who may manage environments.
  secretKeys?: Record<string, string[]>;
};

// Builder is the page that draws a scenario: the nodes to pick from on the left,
// the canvas in the middle, the selected node's fields on the right, and above
// them the bar that says which scenario this is and saves, checks and runs it.
export function Builder(props: BuilderProps) {
  return (
    <ReactFlowProvider>
      <BuilderView {...props} />
    </ReactFlowProvider>
  );
}

function BuilderView({
  projectId,
  projectName,
  scenario,
  targets,
  canEdit,
  canRun,
  now,
  endpoints,
  secretKeys,
}: BuilderProps) {
  const readOnly = !canEdit;
  const flow = useReactFlow();
  const surface = useRef<HTMLDivElement>(null);

  const [graph, setGraph] = useState<ScenarioGraph>(scenario.graph);
  const [box, setBox] = useState<Record<string, Box>>(() => placesToBox(layout(scenario.graph)));
  const [history, setHistory] = useState<History<Snapshot>>(emptyHistory);
  const [baseline, setBaseline] = useState(() => canonical(scenario.graph));
  // Changes when an undo rewrites a node, so the inspector starts from it again.
  const [revision, setRevision] = useState(0);
  const [message, setMessage] = useState<Message | null>(null);
  const [environmentId, setEnvironmentId] = useState(targets[0]?.id ?? "");
  const [pending, startTransition] = useTransition();
  // The field being typed in: a run of keystrokes in one field is one undo step.
  const editing = useRef<string | null>(null);
  const reason = useId();

  // Fit the graph once, when every node has been measured. The `fitView` prop of
  // React Flow runs before that when the nodes are controlled, and fits to nothing.
  const measured = useNodesInitialized();
  const fitted = useRef(false);
  useEffect(() => {
    if (!measured || fitted.current) return;
    fitted.current = true;
    flow.fitView(fitOptions);
  }, [measured, flow]);

  const dirty = useMemo(() => canonical(graph) !== baseline, [graph, baseline]);
  const guard = useUnsavedGuard(dirty);
  // What is wrong with a node: how it is wired, or what a request holds. The
  // API judges the graph at the end; this says it while somebody is still on it.
  const { problems, blocking } = useMemo(() => {
    const found = issues(graph);
    let count = 0;
    for (const node of graph.nodes) {
      if (node.kind !== "http_request") continue;
      const said = requestProblems(node);
      if (!hasProblems(said)) continue;
      count++;
      found[node.id] = [found[node.id], summaryOfProblems(said)].filter(Boolean).join(". ");
    }
    return { problems: found, blocking: count };
  }, [graph]);
  const empty = graph.nodes.length === 0;

  const selected = graph.nodes.filter((node) => box[node.id]?.selected);
  const current = selected.length === 1 ? selected[0] : null;

  // Stable per node while the graph does not change, so that moving a node on the
  // canvas redraws that node and not the other forty-nine.
  const data = useMemo(() => {
    const byId: Record<string, StepData> = {};
    for (const node of graph.nodes) byId[node.id] = { node, issue: problems[node.id], start: node.id === graph.start };
    return byId;
  }, [graph, problems]);

  const nodes = useMemo<StepFlowNode[]>(
    () =>
      graph.nodes.map((node) => {
        const placed = box[node.id] ?? { position: { x: 0, y: 0 } };
        const { title } = describe(node);
        const issue = problems[node.id];
        return {
          id: node.id,
          type: "step",
          ...placed,
          data: data[node.id],
          ariaLabel: `${infoOf(node.kind).label}: ${title}${issue ? `. Needs attention: ${issue}` : ""}`,
          draggable: !readOnly,
          connectable: !readOnly,
          deletable: !readOnly,
        };
      }),
    [graph.nodes, box, data, problems, readOnly],
  );

  const edges = useMemo<Edge[]>(
    () =>
      graph.edges.map((edge) => ({
        id: edgeId(edge),
        source: edge.from,
        target: edge.to,
        sourceHandle: edge.when ?? null,
        label: edge.when,
        markerEnd: { type: "arrowclosed" as const },
        deletable: !readOnly,
        focusable: false,
      })),
    [graph.edges, readOnly],
  );

  const places = useCallback(
    (g: ScenarioGraph) => Object.fromEntries(g.nodes.map((n) => [n.id, box[n.id]?.position ?? { x: 0, y: 0 }])),
    [box],
  );

  // What the graph is now becomes an undo step. Asked twice for the same graph in
  // one breath — a deleted node and then its wires — it is one step.
  function rememberNow() {
    setHistory((h) => (h.past.at(-1)?.graph === graph ? h : remember(h, { graph, places: places(graph) })));
  }

  // Every change but typing goes through here: what the graph was becomes an undo
  // step, and the next keystroke in a field starts a new one.
  function apply(next: ScenarioGraph, nextBox?: Record<string, Box>) {
    rememberNow();
    editing.current = null;
    setMessage(null);
    setGraph(next);
    if (nextBox) setBox(nextBox);
  }

  function edit(id: string, patch: Partial<GraphNode>, field: string) {
    const key = `${id}:${field}`;
    if (editing.current !== key) {
      rememberNow();
      editing.current = key;
    }
    setMessage(null);
    setGraph({ ...graph, nodes: graph.nodes.map((n) => (n.id === id ? { ...n, ...patch } : n)) });
  }

  function restore(snapshot: Snapshot) {
    setGraph(snapshot.graph);
    setBox((b) => {
      const next: Record<string, Box> = {};
      for (const node of snapshot.graph.nodes) next[node.id] = { ...b[node.id], position: snapshot.places[node.id] ?? b[node.id]?.position ?? { x: 0, y: 0 } };
      return next;
    });
    setRevision((r) => r + 1);
    editing.current = null;
    setMessage(null);
  }

  function goBack() {
    const step = undo(history, { graph, places: places(graph) });
    if (!step) return;
    setHistory(step.history);
    restore(step.value);
  }

  function goForward() {
    const step = redo(history, { graph, places: places(graph) });
    if (!step) return;
    setHistory(step.history);
    restore(step.value);
  }

  // Ctrl or Cmd + Z, and Shift to redo. Left alone inside a field, where the
  // browser undoes the typing.
  useEffect(() => {
    if (readOnly) return;
    const onKey = (event: KeyboardEvent) => {
      const inField = event.target instanceof Element && event.target.closest("input, textarea, select");
      if (!(event.metaKey || event.ctrlKey) || inField) return;
      const key = event.key.toLowerCase();
      if (key === "z" && !event.shiftKey) goBack();
      else if ((key === "z" && event.shiftKey) || key === "y") goForward();
      else return;
      event.preventDefault();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  });

  function addNode(kind: NodeKind, at?: Position, wired = true) {
    const id = nextId(graph.nodes);
    const node = newNode(kind, id);
    const anchor = current;

    let position = at;
    if (!position) {
      const origin = anchor ? box[anchor.id]?.position : undefined;
      if (origin) {
        const taken = new Set(graph.nodes.map((n) => `${box[n.id]?.position.x},${box[n.id]?.position.y}`));
        position = { x: origin.x + columnWidth, y: origin.y };
        while (taken.has(`${position.x},${position.y}`)) position = { x: position.x, y: position.y + rowHeight };
      } else {
        const rect = surface.current?.getBoundingClientRect();
        position = rect
          ? flow.screenToFlowPosition({ x: rect.left + rect.width / 2 - 112, y: rect.top + rect.height / 2 - 40 })
          : { x: 0, y: 0 };
      }
    }

    let nextGraph: ScenarioGraph = { ...graph, nodes: [...graph.nodes, node], start: empty ? id : graph.start };
    const exit = anchor && wired ? freeExit(anchor, graph.edges) : null;
    if (anchor && exit) nextGraph = connect(nextGraph, { from: anchor.id, to: id, ...exit });

    const unselected = Object.fromEntries(Object.entries(box).map(([key, value]) => [key, { ...value, selected: false }]));
    apply(nextGraph, { ...unselected, [id]: { position, selected: true } });
    return { graph: nextGraph, id, position };
  }

  // The first request of an empty canvas comes with an end to wire it to: a
  // scenario with no end cannot be saved, and nobody should have to know that
  // before drawing their first call.
  function addFirstRequest() {
    const request = newNode("http_request", "n1");
    const end = newNode("end", "n2");
    const origin = flow.screenToFlowPosition({
      x: (surface.current?.getBoundingClientRect().left ?? 0) + 120,
      y: (surface.current?.getBoundingClientRect().top ?? 0) + 160,
    });
    apply(
      { start: "n1", nodes: [request, end], edges: [{ from: "n1", to: "n2" }] },
      { n1: { position: origin, selected: true }, n2: { position: { x: origin.x + columnWidth, y: origin.y } } },
    );
  }

  function onNodesChange(changes: NodeChange<StepFlowNode>[]) {
    const kept = changes.filter((change) => change.type !== "remove");
    if (kept.length > 0) {
      setBox((b) => {
        const shown = applyNodeChanges(kept, boxNodes(graph.nodes, b, data));
        const next = { ...b };
        for (const node of shown) {
          next[node.id] = { position: node.position, selected: node.selected, dragging: node.dragging, measured: node.measured };
        }
        return next;
      });
    }

    // Deleting from the keyboard is one step to undo, however many nodes and wires
    // went with it: React Flow reports the nodes first, then their wires.
    const removed = new Set(changes.flatMap((change) => (change.type === "remove" ? [change.id] : [])));
    if (removed.size > 0) {
      rememberNow();
      setGraph((g) => without(g, removed));
    }
  }

  function onEdgesChange(changes: EdgeChange[]) {
    const removed = new Set(changes.flatMap((change) => (change.type === "remove" ? [change.id] : [])));
    if (removed.size > 0) {
      rememberNow();
      setGraph((g) => ({ ...g, edges: g.edges.filter((edge) => !removed.has(edgeId(edge))) }));
    }
  }

  function onConnect(connection: Connection) {
    if (readOnly || !connection.source || !connection.target) return;
    const when = connection.sourceHandle as GraphEdge["when"] | null;
    apply(connect(graph, { from: connection.source, to: connection.target, ...(when ? { when } : {}) }));
  }

  // Choosing an endpoint of the catalog fills a request in, in one step to undo.
  function pickEndpoint(node: GraphNode, endpoint: CatalogEndpoint) {
    const filled = fromEndpoint(endpoint);

    // The endpoint's headers replace the node's, except a credential the person
    // has already pointed at a secret: losing Authorization: Bearer {{token}} to a
    // pick would be a quiet way to send the next run unauthenticated.
    const kept = Object.fromEntries(Object.entries(node.headers ?? {}).filter(([name]) => isCredentialHeader(name)));
    const headers = { ...filled.headers, ...kept };

    apply({
      ...graph,
      nodes: graph.nodes.map((n) =>
        n.id === node.id ? { ...n, ...filled, headers: Object.keys(headers).length > 0 ? headers : undefined } : n,
      ),
    });
    setRevision((r) => r + 1);
  }

  // Binding reads a secret into a new "Set variable" step just before the request,
  // and writes {{variable}} where it was asked for, together: one step to undo. The
  // step is placed above the request, out of the way of what is already drawn.
  function bindSecret(node: GraphNode, key: string, patchFor: (variable: string) => Partial<GraphNode>) {
    const id = nextId(graph.nodes);
    const { graph: bound, variable } = bindSecretBefore(graph, node.id, key, id);
    const patch = patchFor(variable);

    const at = box[node.id]?.position ?? { x: 0, y: 0 };
    const unselected = Object.fromEntries(Object.entries(box).map(([k, v]) => [k, v]));
    apply(
      { ...bound, nodes: bound.nodes.map((n) => (n.id === node.id ? { ...n, ...patch } : n)) },
      { ...unselected, [id]: { position: { x: at.x, y: at.y - rowHeight } } },
    );
    setRevision((r) => r + 1);
  }

  function drop(event: React.DragEvent) {
    event.preventDefault();
    const kind = event.dataTransfer.getData(dragType) as NodeKind;
    if (readOnly || !kind) return;
    addNode(kind, flow.screenToFlowPosition({ x: event.clientX - 112, y: event.clientY - 30 }), false);
  }

  function removeCurrent() {
    if (!current) return;
    apply(without(graph, new Set([current.id])));
  }

  function check() {
    setMessage(null);
    startTransition(async () => {
      const outcome = await validateGraph(projectId, scenario.id, graph);
      setMessage(
        outcome.error
          ? { kind: "error", text: outcome.error }
          : { kind: "ok", text: "This graph is valid: a save would be accepted." },
      );
    });
  }

  function save() {
    setMessage(null);
    startTransition(async () => {
      const outcome = await saveGraph(projectId, scenario.id, graph);
      if (outcome.error) {
        setMessage({ kind: "error", text: outcome.error });
        return;
      }
      setBaseline(canonical(graph));
      setMessage({ kind: "ok", text: "Saved as a new version." });
    });
  }

  function run() {
    setMessage(null);
    startTransition(async () => {
      const outcome = await launchDefaultRun(projectId, scenario.id, environmentId);
      if (outcome?.error) setMessage({ kind: "error", text: outcome.error });
    });
  }

  const blocked = runBlockedReason({ dirty, canRun, verifiedEnvironments: targets.length });
  const own = scenario.default_params.target_vus >= 1;

  return (
    <div className="flex flex-col gap-3">
      {guard}

      <div className="flex flex-col gap-2">
        <Link href={`/projects/${projectId}/scenarios/${scenario.id}`} className="w-fit text-xs text-muted-foreground hover:underline">
          {projectName} · Scenarios · {scenario.name}
        </Link>

        <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
          <h1 className="text-xl font-medium tracking-tight">{scenario.name}</h1>
          <span className="data text-xs text-muted-foreground">v{scenario.version}</span>
          <span role="status" className="data rounded-md border px-2 py-0.5 text-[11px] text-muted-foreground">
            {dirty ? (
              "draft · unsaved changes"
            ) : (
              <>
                saved <Time iso={scenario.updated_at} mode="relative" now={now} />
              </>
            )}
          </span>
          <span className="data text-xs text-muted-foreground" title="The default profile a run starts from">
            {own ? profileSummary(scenario.default_profile, scenario.default_params) : `${scenario.default_profile} template`}
          </span>

          <div className="ml-auto flex flex-wrap items-center gap-2">
            {canRun && (
              <label className="flex items-center gap-2 text-xs text-muted-foreground">
                target
                <select
                  value={environmentId}
                  onChange={(event) => setEnvironmentId(event.target.value)}
                  disabled={targets.length === 0}
                  aria-describedby={blocked ? reason : undefined}
                  className="data h-8 rounded-md border bg-background px-2 text-xs text-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50"
                >
                  {targets.length === 0 && <option value="">no verified environment</option>}
                  {targets.map((target) => (
                    <option key={target.id} value={target.id}>
                      {target.name}
                    </option>
                  ))}
                </select>
              </label>
            )}
            {canEdit && (
              <>
                <Button variant="outline" onClick={check} disabled={pending || empty}>
                  Validate
                </Button>
                <Button variant="outline" onClick={save} disabled={pending || !dirty || empty || blocking > 0}>
                  Save
                </Button>
              </>
            )}
            {canRun && (
              <Button onClick={run} disabled={blocked !== null || pending || !environmentId || empty} aria-describedby={blocked ? reason : undefined}>
                <Play aria-hidden />
                Run
              </Button>
            )}
          </div>
        </div>

        {canRun && blocked && (
          <p id={reason} className="text-xs text-muted-foreground">
            {blocked}
          </p>
        )}
        {canEdit && blocking > 0 && (
          <p className="text-xs text-muted-foreground">
            {blocking === 1 ? "One request needs attention" : `${blocking} requests need attention`} before this can be saved:
            each says what is wrong on the canvas.
          </p>
        )}
        {message && (
          <p role={message.kind === "error" ? "alert" : "status"} className="text-xs text-muted-foreground">
            {message.text}
          </p>
        )}
        {readOnly && (
          <p className="text-xs text-muted-foreground">You can look at this scenario but not change it: that takes the maintainer role.</p>
        )}
      </div>

      <div className={`grid min-h-[34rem] gap-3 lg:h-[calc(100svh-14rem)] ${readOnly ? "lg:grid-cols-[minmax(0,1fr)_20rem]" : "lg:grid-cols-[12.5rem_minmax(0,1fr)_20rem]"}`}>
        {!readOnly && <Palette onAdd={(kind) => addNode(kind)} />}

        <div ref={surface} className="relative min-h-[26rem] overflow-hidden rounded-xl border bg-background">
          {empty && <EmptyState projectId={projectId} readOnly={readOnly} onAdd={addFirstRequest} />}
          <ReactFlow
            className="builder-flow"
            aria-label="Scenario canvas"
            nodes={nodes}
            edges={edges}
            nodeTypes={nodeTypes}
            onNodesChange={onNodesChange}
            onEdgesChange={onEdgesChange}
            onConnect={onConnect}
            isValidConnection={(connection) => connection.source !== connection.target}
            onDragOver={(event) => {
              event.preventDefault();
              event.dataTransfer.dropEffect = "move";
            }}
            onDrop={drop}
            onNodeDragStart={() => {
              if (!readOnly) rememberNow();
            }}
            nodesDraggable={!readOnly}
            nodesConnectable={!readOnly}
            deleteKeyCode={readOnly ? null : ["Backspace", "Delete"]}
            minZoom={0.1}
            maxZoom={1.5}
            zoomOnDoubleClick={false}
          >
            <Background variant={BackgroundVariant.Dots} gap={20} size={1.5} />
            <MiniMap pannable zoomable ariaLabel="Mini map of the scenario" nodeColor="var(--muted-foreground)" />
            <ZoomPanel />
            {!readOnly && (
              <Panel position="top-left" className="flex gap-1">
                <Button variant="outline" size="icon" aria-label="Undo" onClick={goBack} disabled={history.past.length === 0}>
                  <Undo2 aria-hidden />
                </Button>
                <Button variant="outline" size="icon" aria-label="Redo" onClick={goForward} disabled={history.future.length === 0}>
                  <Redo2 aria-hidden />
                </Button>
              </Panel>
            )}
          </ReactFlow>
        </div>

        {current ? (
          <Inspector
            node={current}
            readOnly={readOnly}
            revision={revision}
            onChange={(patch, field) => edit(current.id, patch, field)}
            onDelete={removeCurrent}
            onClose={() => setBox((b) => Object.fromEntries(Object.entries(b).map(([k, v]) => [k, { ...v, selected: false }])))}
            request={{
              endpoints,
              endpointsHref: `/projects/${projectId}/endpoints`,
              available: variablesBefore(graph, current.id),
              unbound: unboundSecrets(secretKeys?.[environmentId] ?? [], graph),
              script: scenario.script,
              onPickEndpoint: (endpoint) => pickEndpoint(current, endpoint),
              onBind: (key, patchFor) => bindSecret(current, key, patchFor),
            }}
          />
        ) : (
          <aside aria-label="Node inspector" className="rounded-xl border bg-card p-4 text-sm text-muted-foreground">
            {empty ? "Nothing to inspect yet." : "Select a node to see and change what it holds."}
          </aside>
        )}
      </div>
    </div>
  );
}

// boxNodes is the nodes as React Flow last had them, for applying its changes to.
function boxNodes(nodes: GraphNode[], box: Record<string, Box>, data: Record<string, StepData>): StepFlowNode[] {
  return nodes.map((node) => ({
    id: node.id,
    type: "step" as const,
    position: box[node.id]?.position ?? { x: 0, y: 0 },
    selected: box[node.id]?.selected,
    dragging: box[node.id]?.dragging,
    measured: box[node.id]?.measured,
    data: data[node.id],
  }));
}

function placesToBox(places: Record<string, Position>): Record<string, Box> {
  return Object.fromEntries(Object.entries(places).map(([id, position]) => [id, { position }]));
}

// ZoomPanel is the zoom out, the zoom level, zoom in and Fit.
function ZoomPanel() {
  const { zoomIn, zoomOut, fitView } = useReactFlow();
  const zoom = useStore((state) => state.transform[2]);

  return (
    <Panel position="bottom-left" className="flex items-center gap-1 rounded-lg border bg-card p-1">
      <Button variant="ghost" size="icon-sm" aria-label="Zoom out" onClick={() => zoomOut()}>
        −
      </Button>
      <span className="num w-10 text-center text-xs" aria-live="polite">
        {Math.round(zoom * 100)}%
      </span>
      <Button variant="ghost" size="icon-sm" aria-label="Zoom in" onClick={() => zoomIn()}>
        +
      </Button>
      <Button variant="ghost" size="sm" onClick={() => fitView(fitOptions)}>
        Fit
      </Button>
    </Panel>
  );
}
