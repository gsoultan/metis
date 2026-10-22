import {
  ReactFlow,
  Background,
  BackgroundVariant,
  Panel,
  MiniMap,
  type Node,
  type Edge,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import {
  Alert,
  Button,
  CloseButton,
  Group,
  Text,
  Badge,
  Notification,
  Box,
} from '@mantine/core';
import { Save, MousePointer2 } from 'lucide-react';
import { CanvasToolbar } from '../components/designer/CanvasToolbar';
import { DesignerHeader } from '../components/designer/DesignerHeader';
import { EmptyCanvasHint } from '../components/designer/EmptyCanvasHint';
import { DesignerSidebar } from '../components/DesignerSidebar';
import { NodeActionsContext } from '../components/designer/NodeActionsContext';
import classes from './ProcessDesigner.module.css';
import { PropertyPanel } from '../components/PropertyPanel';
import { DesignerModals } from '../components/DesignerModals';
import { DeployVersionModal } from '../components/DeployVersionModal';
import { nodeTypes } from '../components/bpmnNodeTypes';
import { SimulationAnswerCard, SimulationRail, SimulationTransport } from '../components/simulation';
import { useNavigate, useSearch } from '@tanstack/react-router';
import { useCallback, useEffect, useMemo, useRef } from 'react';
import { decorateEdges, decorateNodes } from '../domain/simulationCanvas';
import { answerKindFor } from '../domain/simulationScenario';
import { canFollow } from '../domain/canvasPlacement';
import { useProcessDesigner } from '../hooks/useProcessDesigner';
import { useSimulation } from '../hooks/useSimulation';
import { useSimulationCases } from '../hooks/useSimulationCases';
import { useSimulationTarget } from '../hooks/useSimulationTarget';
import { useAppStore } from '../store/useAppStore';
import type { BPMNNodeData, BPMNEdgeData } from '../types/bpmn';

/** A fit that keeps the diagram clear of the cards floating over the canvas. */
const FIT = {
  padding: { top: '96px', left: '150px', right: '120px', bottom: '48px' },
  maxZoom: 1.1,
} as const;

export function ProcessDesigner({
  definitionId,
  instanceId,
  onViewInstance,
}: {
  definitionId?: string | null;
  instanceId?: string | null;
  onViewInstance?: (id: string, defId: string) => void;
}) {
  const search = useSearch({ from: '/_authenticated/designer' });

  const designer = useProcessDesigner({
    definitionId,
    instanceId,
    initialName: search.name,
    initialKey: search.key,
    initialTemplate: search.template,
  });

  const {
    nodes, edges,
    selectedNode, selectedEdge,
    processName, processKey,
    reactFlowInstance, setReactFlowInstance,
    componentsOpened, openComponents, closeComponents, toggleComponents,
    spotlightOpened, closeSpotlight,
    checklistOpened, closeChecklist,
    rolloutOpened, closeRollout, versions, deploying,
    clearCanvasOpened, closeClearCanvas, confirmClearCanvas,
    lastSaved,
    history, historyIndex,
    issues,
    remoteCursors,
    createDefinition, exportMutation, importMutation,
    fileInputRef,
    onMouseMove, handleProcessNameChange,
    onNodesChange, onNodeDragStop, onEdgesChange, onConnect,
    onDragOver, onDrop, addNodeAt, selectById,
    onNodeClick, onEdgeClick, onPaneClick,
    deleteSelected, clearCanvas, onAutoLayout,
    updateNodeData, updateEdgeData,
    proceedWithSave, deployAnyway, onSave, onExport, onImport,
    offeredDraft, offeredDraftAge, restoreDraft, discardDraft,
    handleFileChange,
    undo, redo,
  } = designer;

  // All state and handlers come from the hook — no duplicate logic here.

  /**
   * Issues that make a deploy fail rather than merely warn.
   *
   * Warnings stay deployable on purpose: a process with an unreachable node is
   * odd but runnable, and refusing to save it would trap somebody mid-edit.
   */
  const blockingIssues = issues.filter((issue) => issue.severity === 'error').length;

  /*
   * Simulation mode.
   *
   * Runs the canvas: the loaded version while the canvas still matches it, a
   * draft once it does not. Either way the run happens on the server, on the
   * real engine, and nothing it does is kept. See useSimulationTarget.
   */
  const navigate = useNavigate();
  const currentProjectId = useAppStore((state) => state.currentProjectId);
  const simulating = search.mode === 'simulate';

  const simulationTarget = useSimulationTarget({
    active: simulating,
    projectId: currentProjectId,
    deployed: designer.deployed,
    processName,
    processKey,
    nodes,
    edges,
  });

  const sim = useSimulation(simulationTarget);
  const savedCases = useSimulationCases(sim, currentProjectId, processKey, simulating);

  /*
   * The canvas the simulation draws on is the designer's own, dressed. Nothing
   * about the model changes — only opacity, outlines and which edges animate —
   * so switching back to Design leaves exactly what was there.
   */
  const decoration = useMemo(
    () => ({
      // Dressed as soon as the mode is on, not only once a run exists: the
      // answers somebody has already given show on the diagram before the first
      // Run, which is how they can see what is set up.
      active: simulating,
      nodeStates: sim.nodeStates,
      flowStates: sim.flowStates,
      tokens: sim.step?.tokens ?? [],
      awaiting: sim.editing ?? sim.run?.awaitingNode ?? null,
      answered: sim.answered,
    }),
    [simulating, sim.nodeStates, sim.flowStates, sim.step, sim.editing, sim.run, sim.answered],
  );

  const canvasNodes = useMemo(() => decorateNodes(nodes, decoration), [nodes, decoration]);
  const canvasEdges = useMemo(() => decorateEdges(edges, decoration), [edges, decoration]);

  /*
   * Clicking a step in simulate mode answers it.
   *
   * Only steps that have something outside them to answer for — a person, a
   * service, a message. A gateway or a decision table runs for real, so there
   * is nothing to say about it and clicking one does nothing rather than
   * opening an empty card.
   */
  const answerNode = useCallback(
    (nodeId: string) => {
      const node = nodes.find((candidate) => candidate.id === nodeId);
      const kind = answerKindFor(node?.type);
      if (kind === null) return;
      sim.startAnswering(nodeId, kind);
    },
    [nodes, sim],
  );

  const onSimulationNodeClick = useCallback(
    (_event: React.MouseEvent, node: Node<BPMNNodeData>) => answerNode(node.id),
    [answerNode],
  );

  /** What the diagram calls a node, for the sentences simulation writes. */
  const labelOf = useCallback(
    (nodeId: string) => {
      const found = nodes.find((node) => node.id === nodeId);
      return typeof found?.data.label === 'string' && found.data.label !== '' ? found.data.label : nodeId;
    },
    [nodes],
  );

  const awaitingNode = sim.editing;
  const awaitingLabel = useMemo(() => {
    const found = nodes.find((node) => node.id === awaitingNode);
    return typeof found?.data.label === 'string' ? found.data.label : (awaitingNode ?? '');
  }, [nodes, awaitingNode]);

  const deployBlockedBy =
    nodes.length === 0
      ? 'Add at least one step before deploying'
      : blockingIssues > 0
        ? `Fix ${blockingIssues} ${blockingIssues === 1 ? 'problem' : 'problems'} before deploying`
        : null;

  const nodeActions = useMemo(
    () => ({ addAfter: designer.addNodeAfter, remove: designer.removeNode }),
    [designer.addNodeAfter, designer.removeNode],
  );

  /** The middle of the visible canvas, on screen: where a clicked step lands. */
  const canvasRef = useRef<HTMLDivElement>(null);
  const addAtCentre = useCallback(
    (type: string, data: Partial<BPMNNodeData> = {}) => {
      const bounds = canvasRef.current?.getBoundingClientRect();
      if (!bounds) return;
      // Offset by roughly half a step, so the step's middle — not its corner —
      // lands in the middle of the view.
      addNodeAt(type, data, { x: bounds.left + bounds.width / 2 - 90, y: bounds.top + bounds.height / 2 - 40 });
    },
    [addNodeAt],
  );

  /*
   * Keeps the selected step in view.
   *
   * The settings now open beside the diagram instead of over it, which takes
   * 440px off the right of the canvas — often exactly where the step just
   * clicked was. If the step ends up behind the panel, or off any other edge,
   * the view pans to it, at the zoom the person chose.
   */
  const selectedId = selectedNode?.id ?? null;
  useEffect(() => {
    if (selectedId === null || reactFlowInstance === null) return;
    // After layout, so the canvas has already narrowed for the panel.
    const frame = requestAnimationFrame(() => {
      const node = reactFlowInstance.getInternalNode(selectedId);
      const bounds = canvasRef.current?.getBoundingClientRect();
      if (!node || !bounds) return;
      const { zoom } = reactFlowInstance.getViewport();
      const at = node.internals.positionAbsolute;
      const width = node.measured.width ?? 180;
      const height = node.measured.height ?? 60;
      const corner = reactFlowInstance.flowToScreenPosition(at);
      const margin = 24;
      // The settings card covers the right of the canvas, the cards at the
      // top its first 64px, and the tool rail its left edge.
      const hidden =
        corner.x < bounds.left + 72 + margin ||
        corner.y < bounds.top + 64 + margin ||
        corner.x + width * zoom > bounds.right - 434 - margin ||
        corner.y + height * zoom > bounds.bottom - margin;
      if (hidden) {
        // Centred in the part of the canvas the card leaves showing.
        const shift = (434 - 72) / 2 / zoom;
        void reactFlowInstance.setCenter(at.x + width / 2 + shift, at.y + height / 2, { zoom, duration: 300 });
      }
    });
    return () => cancelAnimationFrame(frame);
  }, [selectedId, reactFlowInstance]);

  return (
    <Box h="calc(100vh - 60px)" style={{ display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
      {offeredDraft && (
        /*
          Unsaved work this browser kept after the last deploy. It is offered
          rather than applied: the draft could be an abandoned experiment, and
          silently replacing what the server returned would be worse than
          losing it.
        */
        <Alert
          color="blue"
          variant="light"
          radius={0}
          title="You have unsaved changes to this process"
          icon={<Save size={18} />}
        >
          <Group justify="space-between" align="center" wrap="nowrap">
            <Text size="sm">
              This browser kept changes {offeredDraftAge} that were never deployed.
            </Text>
            <Group gap="xs" wrap="nowrap">
              <Button size="xs" variant="filled" onClick={restoreDraft}>
                Restore them
              </Button>
              <Button size="xs" variant="subtle" color="gray" onClick={discardDraft}>
                Discard
              </Button>
            </Group>
          </Group>
        </Alert>
      )}

      <input
        type="file"
        ref={fileInputRef}
        style={{ display: 'none' }}
        accept=".bpmn,.xml"
        onChange={handleFileChange}
      />

      <Box style={{ flex: 1, display: 'flex', minHeight: 0, overflow: 'hidden' }}>
        <Box ref={canvasRef} className={classes.canvas}>
        <NodeActionsContext.Provider value={simulating ? null : nodeActions}>
        <ReactFlow<Node<BPMNNodeData>, Edge<BPMNEdgeData>>
          proOptions={{ hideAttribution: true }}
          nodes={canvasNodes}
          edges={canvasEdges}
          onNodesChange={onNodesChange}
          onEdgesChange={onEdgesChange}
          onConnect={onConnect}
          onInit={setReactFlowInstance}
          onMouseMove={onMouseMove}
          onDrop={onDrop}
          onDragOver={onDragOver}
          onNodeClick={simulating ? onSimulationNodeClick : onNodeClick}
          onNodeDragStop={onNodeDragStop}
          onEdgeClick={onEdgeClick}
          onPaneClick={simulating ? sim.stopAnswering : onPaneClick}
          nodeTypes={nodeTypes}
          fitView
          fitViewOptions={FIT}
          style={{ width: '100%', height: '100%' }}
        >
          <Background
            variant={BackgroundVariant.Dots}
            gap={22}
            size={1.4}
            color="light-dark(var(--mantine-color-gray-4), var(--mantine-color-dark-4))"
          />
          {/* Only once there is something to navigate. On an empty canvas the
              minimap is a blank rectangle floating over the grid — it reads as
              a rendering fault rather than as an empty map, which is the first
              thing a new user sees. */}
          {nodes.length > 0 && (
          <MiniMap
            pannable
            zoomable
            ariaLabel="Overview of the whole diagram"
            position="bottom-left"
            style={{ marginLeft: 76 }}
            className={classes.minimap}
            maskColor="light-dark(rgba(241, 243, 245, 0.7), rgba(26, 27, 30, 0.7))"
            nodeBorderRadius={6}
            nodeStrokeWidth={2}
            nodeStrokeColor={(n) => {
              if (n.type === 'startEvent') return '#40c057';
              if (n.type === 'endEvent') return '#fa5252';
              return '#748ffc';
            }}
            nodeColor={(n) => {
              if (n.type === 'startEvent') return '#ebfbee';
              if (n.type === 'endEvent') return '#fff5f5';
              return '#edf2ff';
            }}
          />
          )}

          {/* Remote Cursors Overlay */}
          {Object.entries(remoteCursors).map(([id, cursor]) => (
            <Box
              key={id}
              style={{
                position: 'absolute',
                left: cursor.x,
                top: cursor.y,
                pointerEvents: 'none',
                zIndex: 1000,
                transition: 'all 0.05s linear'
              }}
            >
              <MousePointer2 size={16} fill="var(--mantine-color-blue-6)" color="white" />
              <Badge size="xs" variant="filled" color="blue" style={{ marginLeft: 8, transform: 'translateY(-10px)' }}>{cursor.name}</Badge>
            </Box>
          ))}


          {nodes.length === 0 && !simulating && (
            <Panel position="top-center" style={{ marginTop: 96 }}>
              <EmptyCanvasHint
                paletteOpen={componentsOpened}
                onAddStart={() => addAtCentre('startEvent', {})}
                onShowPalette={openComponents}
              />
            </Panel>
          )}

          {/*
            The question a step is asking, anchored to the step. NodeToolbar has
            to live inside ReactFlow to know where its node is, which is why it
            is here rather than beside the rail.
          */}
          {simulating && awaitingNode !== null && (
            <SimulationAnswerCard sim={sim} nodeId={awaitingNode} label={awaitingLabel} />
          )}
        </ReactFlow>
        </NodeActionsContext.Provider>

          <DesignerHeader
                floating
            processName={processName}
            onNameChange={handleProcessNameChange}
            processKey={processKey}
            lastSaved={lastSaved}
            simulating={simulating}
            onModeChange={(mode) =>
              navigate({
                to: '/designer',
                search: { ...search, mode },
                replace: true,
              })
            }
            issues={issues}
            onSelectIssue={selectById}
            onBack={() => navigate({ to: '/models', search: { tab: 'processes' } })}
            onImport={onImport}
            onExport={onExport}
            importing={importMutation.isPending}
            exporting={exportMutation.isPending}
            onDeploy={onSave}
            deploying={createDefinition.isPending}
            deployBlockedBy={deployBlockedBy}
          />

          {/*
            Canvas first: the diagram runs edge to edge, and everything else
            floats over it — the name and actions in two cards at the top, the
            tools in a rail down the left, the building blocks in a flyout from
            its "+", and a step's settings in a card on the right.
          */}
          {!simulating && (
            <Box className={classes.rail}>
              <CanvasToolbar
                vertical
                paletteOpen={componentsOpened}
                onTogglePalette={toggleComponents}
                canUndo={historyIndex > 0}
                canRedo={historyIndex < history.length - 1}
                undoSteps={Math.max(historyIndex, 0)}
                onUndo={undo}
                onRedo={redo}
                onZoomIn={() => reactFlowInstance?.zoomIn()}
                onZoomOut={() => reactFlowInstance?.zoomOut()}
                onFit={() => reactFlowInstance?.fitView(FIT)}
                onAutoLayout={onAutoLayout}
                onClear={clearCanvas}
              />
            </Box>
          )}

          {!simulating && componentsOpened && (
            <Box component="aside" aria-label="Building blocks" className={classes.flyout}>
              <Group justify="space-between" wrap="nowrap" mb={2}>
                <Text fw={700} size="sm">
                  Add a step
                </Text>
                <CloseButton size="sm" aria-label="Hide building blocks" onClick={closeComponents} />
              </Group>
              {/* Says what a click will do, because with a step selected it
                  does more than add: it joins the new step on after it. */}
              <Text size="xs" c="dimmed" mb="sm" lineClamp={2}>
                {selectedNode && canFollow(String(selectedNode.type))
                  ? `Click to add after “${labelOf(selectedNode.id)}”, or drag onto the canvas`
                  : 'Click to add, or drag onto the canvas'}
              </Text>
              <Box style={{ flex: 1, minHeight: 0 }}>
                <DesignerSidebar embedded onAdd={addAtCentre} />
              </Box>
            </Box>
          )}

          {/*
            Not in simulate mode: a run is a trace of the model as it was when
            the run started, and editing mid-trace would leave the canvas
            showing a diagram the steps below it never ran on.
          */}
          {!simulating && (
            <PropertyPanel
              floating
              selectedNode={selectedNode}
              selectedEdge={selectedEdge}
              onClose={designer.closeSelection}
              onDelete={deleteSelected}
              updateNodeData={updateNodeData}
              updateEdgeData={updateEdgeData}
              nodes={nodes}
              edges={edges}
              instanceId={instanceId}
              onViewInstance={onViewInstance}
            />
          )}
        </Box>


        {simulating && (
          <Box
            w={340}
            style={{
              flexShrink: 0,
              overflow: 'hidden',
              borderLeft: '1px solid light-dark(var(--mantine-color-gray-2), var(--mantine-color-dark-4))',
              backgroundColor: 'light-dark(var(--mantine-color-white), var(--mantine-color-dark-7))',
            }}
          >
            <SimulationRail
              sim={sim}
              cases={savedCases}
              target={simulationTarget}
              labelOf={labelOf}
              instanceId={instanceId ?? undefined}
              onAnswerAwaiting={answerNode}
            />
          </Box>
        )}
      </Box>

      {simulating && <SimulationTransport sim={sim} />}

      <DesignerModals
        checklistOpened={checklistOpened}
        closeChecklist={closeChecklist}
        issues={issues}
        onDeployAnyway={deployAnyway}
        spotlightOpened={spotlightOpened}
        closeSpotlight={closeSpotlight}
        clearCanvasOpened={clearCanvasOpened}
        closeClearCanvas={closeClearCanvas}
        confirmClearCanvas={confirmClearCanvas}
        onImport={onImport}
        onExport={onExport}
        onSave={onSave}
        onAutoLayout={onAutoLayout}
        undo={undo}
        clearCanvas={clearCanvas}
      />

      {/*
        Asked before a redeploy, never before the first deploy: the choice is
        which version new instances start on, and until one is already live
        there is nothing to choose between.
      */}
      <DeployVersionModal
        opened={rolloutOpened}
        onClose={closeRollout}
        processName={processName}
        versions={versions}
        deploying={deploying}
        onDeploy={proceedWithSave}
      />

      {createDefinition.isSuccess && (
        <Notification 
          icon={<Save size={18} />} 
          color="teal" 
          title="Success" 
          onClose={() => createDefinition.reset()}
          style={{ position: 'fixed', bottom: 20, right: 20, zIndex: 1000 }}
        >
          Process definition deployed successfully!
        </Notification>
      )}
    </Box>
  );
}
