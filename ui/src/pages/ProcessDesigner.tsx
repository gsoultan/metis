import {
  ReactFlow,
  Background,
  Controls,
  Panel,
  MiniMap,
  type Node,
  type Edge,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import {
  Alert,
  Button,
  Group,
  Stack,
  TextInput,
  Text,
  Paper,
  Divider,
  ActionIcon,
  Badge,
  Notification,
  Box,
  Tooltip,
  ScrollArea,
  Title,
} from '@mantine/core';
import {
  Save,
  LayoutGrid,
  Undo2,
  Redo2,
  Maximize,
  ZoomIn,
  ZoomOut,
  AlertCircle,
  MousePointer2,
  Trash,
  FileUp,
  Download,
} from 'lucide-react';
import { PropertyPanel } from '../components/PropertyPanel';
import { DesignerModals } from '../components/DesignerModals';
import { DeployVersionModal } from '../components/DeployVersionModal';
import { nodeTypes } from '../components/bpmnNodeTypes';
import { useSearch } from '@tanstack/react-router';
import { useProcessDesigner } from '../hooks/useProcessDesigner';
import type { BPMNNodeData, BPMNEdgeData } from '../types/bpmn';

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
    componentsOpened, openComponents, closeComponents,
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
    onDragOver, onDrop,
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


  return (
    <Box h="calc(100vh - 60px)" style={{ display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
      <Box
        p="md"
        style={{
          backgroundColor: 'light-dark(var(--mantine-color-white), var(--mantine-color-dark-7))',
          borderBottom: '1px solid light-dark(var(--mantine-color-gray-2), var(--mantine-color-dark-4))',
        }}
      >
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
            mb="sm"
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
        <Group justify="space-between" align="center">
          <Stack gap={0}>
            <Title order={3} fw={800}>{processName || 'Process Designer'}</Title>
            <Group gap="xs">
              <Text size="xs" c="dimmed">Key: {processKey}</Text>
              {/*
                "Last saved" used to sit next to an "Autosaving…" badge that
                could never paint, and both described a localStorage write the
                app then never read. It now says plainly where the draft is and
                that deploying is what publishes it.
              */}
              {lastSaved ? (
                <Text size="xs" c="dimmed">
                  Draft kept in this browser at {lastSaved.toLocaleTimeString()} • Deploy to publish
                </Text>
              ) : (
                <Text size="xs" c="dimmed">Not deployed yet</Text>
              )}
            </Group>
          </Stack>
          <Group>
            <Button 
              variant="light" 
              color="indigo" 
              size="xs"
              leftSection={<LayoutGrid size={14} />}
              onClick={openComponents}
            >
              Add step
            </Button>
            <Button 
              variant="subtle" 
              color="gray" 
              size="xs"
              leftSection={<FileUp size={14} />}
              onClick={onImport}
              loading={importMutation.isPending}
            >
              Import
            </Button>
            <Button 
              variant="subtle" 
              color="gray" 
              size="xs"
              leftSection={<Download size={14} />}
              onClick={onExport}
              loading={exportMutation.isPending}
            >
              Export
            </Button>
            {/*
              Was the filled primary button on an empty, invalid process — the
              most prominent control on screen offering the one action that
              could not succeed. It is now disabled until the process has
              something to deploy and no blocking errors, and says why.
            */}
            <Tooltip
              label={
                nodes.length === 0
                  ? 'Add at least one step before deploying'
                  : blockingIssues > 0
                    ? `Fix ${blockingIssues} validation ${blockingIssues === 1 ? 'error' : 'errors'} before deploying`
                    : 'Deploy this version so new instances can start on it'
              }
            >
              <Button
                variant="filled"
                color="indigo"
                size="xs"
                leftSection={<Save size={14} />}
                onClick={onSave}
                loading={createDefinition.isPending}
                disabled={nodes.length === 0 || blockingIssues > 0}
                data-disabled={nodes.length === 0 || blockingIssues > 0 ? true : undefined}
              >
                Deploy Model
              </Button>
            </Tooltip>
            <input 
              type="file" 
              ref={fileInputRef} 
              style={{ display: 'none' }} 
              accept=".bpmn,.xml" 
              onChange={handleFileChange} 
            />
          </Group>
        </Group>
      </Box>

      <Box style={{ flex: 1, position: 'relative', overflow: 'hidden' }}>
        <ReactFlow<Node<BPMNNodeData>, Edge<BPMNEdgeData>>
          proOptions={{ hideAttribution: true }}
          nodes={nodes}
          edges={edges}
          onNodesChange={onNodesChange}
          onEdgesChange={onEdgesChange}
          onConnect={onConnect}
          onInit={setReactFlowInstance}
          onMouseMove={onMouseMove}
          onDrop={onDrop}
          onDragOver={onDragOver}
          onNodeClick={onNodeClick}
          onNodeDragStop={onNodeDragStop}
          onEdgeClick={onEdgeClick}
          onPaneClick={onPaneClick}
          nodeTypes={nodeTypes}
          fitView
          style={{ width: '100%', height: '100%' }}
        >
          <Background />
          <Controls showInteractive={false} />
          {/* Only once there is something to navigate. On an empty canvas the
              minimap is a blank white rectangle with no border, floating over
              the grid — it reads as a rendering fault rather than as an empty
              map, which is the first thing a new user sees. */}
          {nodes.length > 0 && (
          <MiniMap
            // A border, so it reads as a panel sitting above the canvas rather
            // than as a hole punched in it.
            style={{ border: '1px solid var(--mantine-color-gray-3)', borderRadius: 4 }}
            nodeStrokeColor={(n) => {
              if (n.type === 'startEvent') return '#40c057';
              if (n.type === 'endEvent') return '#fa5252';
              return '#dee2e6';
            }}
            nodeColor={(n) => {
              if (n.type === 'startEvent') return '#ebfbee';
              if (n.type === 'endEvent') return '#fff5f5';
              return '#fff';
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
          
          <Panel position="top-left">
            <Paper p="xs" withBorder radius="md" bg="var(--mantine-color-body)" shadow="xs">
              <Stack gap="xs" w={200}>
                <TextInput 
                  label="Process Name" 
                  placeholder="e.g. Employee Onboarding"
                  size="xs" 
                  value={processName} 
                  onChange={(e) => handleProcessNameChange(e.target.value)} 
                />
              </Stack>
            </Paper>
          </Panel>

          <Panel position="bottom-center">
            <Paper p="4" withBorder radius="xl" bg="var(--mantine-color-body)" shadow="md" mb="md">
              <Group gap="xs">
                <Tooltip label="Selection Mode">
                  <ActionIcon aria-label="Select tool" variant="light" size="lg">
                    <MousePointer2 size={18} />
                  </ActionIcon>
                </Tooltip>
                
                <Divider orientation="vertical" />

                <Tooltip label={`Undo (${historyIndex > 0 ? historyIndex : 0} steps)`}>
                  <ActionIcon aria-label="Undo" 
                    variant="subtle" 
                    size="lg" 
                    disabled={historyIndex <= 0}
                    onClick={undo}
                  >
                    <Undo2 size={18} />
                  </ActionIcon>
                </Tooltip>

                <Tooltip label="Redo">
                  <ActionIcon aria-label="Redo" 
                    variant="subtle" 
                    size="lg" 
                    disabled={historyIndex >= history.length - 1}
                    onClick={redo}
                  >
                    <Redo2 size={18} />
                  </ActionIcon>
                </Tooltip>

                <Divider orientation="vertical" />

                <Tooltip label="Zoom In">
                  <ActionIcon aria-label="Zoom in" variant="subtle" size="lg" onClick={() => reactFlowInstance?.zoomIn()}>
                    <ZoomIn size={18} />
                  </ActionIcon>
                </Tooltip>
                <Tooltip label="Zoom Out">
                  <ActionIcon aria-label="Zoom out" variant="subtle" size="lg" onClick={() => reactFlowInstance?.zoomOut()}>
                    <ZoomOut size={18} />
                  </ActionIcon>
                </Tooltip>
                <Tooltip label="Fit View">
                  <ActionIcon aria-label="Fit diagram to view" variant="subtle" size="lg" onClick={() => reactFlowInstance?.fitView()}>
                    <Maximize size={18} />
                  </ActionIcon>
                </Tooltip>

                <Tooltip label="Auto-Layout">
                  <ActionIcon aria-label="Apply automatic layout" variant="subtle" color="indigo" size="lg" onClick={onAutoLayout}>
                    <LayoutGrid size={18} />
                  </ActionIcon>
                </Tooltip>

                <Divider orientation="vertical" />

                {/* The accessible name said "Delete" while the tooltip said
                    "Clear Canvas" — a screen reader user and a sighted user
                    were told this button did two different things. */}
                <Tooltip label="Clear the canvas">
                  <ActionIcon aria-label="Clear the canvas" variant="subtle" color="red" size="lg" onClick={clearCanvas}>
                    <Trash size={18} />
                  </ActionIcon>
                </Tooltip>
              </Group>
            </Paper>
          </Panel>

          {/*
            An empty canvas is not a validation failure, it is a person who has
            just arrived. The designer used to greet them with a blank grid and
            a 280px panel reading "Validation (1) — This process is empty.",
            which names the problem and offers no way out: the palette that
            holds the steps is behind a "Components" button in the top-right
            toolbar, grouped with Import and Export so it reads as a view
            toggle. bpmn.io and Camunda Modeler both keep that palette open.

            So: say what to do, and give the control that does it.
          */}
          {nodes.length === 0 && (
            <Panel position="top-center">
              <Paper p="md" withBorder radius="md" bg="var(--mantine-color-body)" shadow="sm" style={{ maxWidth: 320 }}>
                <Stack gap="xs">
                  <Text size="sm" fw={700}>Start with a step</Text>
                  <Text size="xs" c="dimmed">
                    A process is a sequence of steps. Add the first one, then drag from its
                    edge to connect the next.
                  </Text>
                  <Button size="xs" leftSection={<LayoutGrid size={14} />} onClick={openComponents}>
                    Add a step
                  </Button>
                </Stack>
              </Paper>
            </Panel>
          )}

          {issues.length > 0 && nodes.length > 0 && (
            <Panel position="bottom-left">
              <Paper
                p="xs"
                withBorder
                radius="md"
                bg="var(--mantine-color-body)"
                shadow="sm"
                style={{ maxWidth: 280 }}
              >
                <Group gap="xs" mb={4}>
                  <AlertCircle size={14} color="var(--mantine-color-orange-6)" />
                  <Text size="xs" fw={700}>Validation ({issues.length})</Text>
                </Group>
                <ScrollArea.Autosize mah={120} type="hover">
                  <Stack gap={4}>
                    {issues.map((issue, idx) => (
                      <Group key={idx} gap={4} wrap="nowrap">
                        <Badge 
                          size="xs" 
                          color={issue.severity === 'error' ? 'red' : 'orange'} 
                          variant="dot"
                        />
                        <Text size="xs" truncate>{issue.message}</Text>
                      </Group>
                    ))}
                  </Stack>
                </ScrollArea.Autosize>
              </Paper>
            </Panel>
          )}
        </ReactFlow>

        <PropertyPanel
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
      </Box>

      <DesignerModals
        checklistOpened={checklistOpened}
        closeChecklist={closeChecklist}
        issues={issues}
        onDeployAnyway={deployAnyway}
        spotlightOpened={spotlightOpened}
        closeSpotlight={closeSpotlight}
        componentsOpened={componentsOpened}
        closeComponents={closeComponents}
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
