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
import { nodeTypes } from '../components/BPMNNodes';
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
  const search = useSearch({ from: '/_authenticated/designer' }) as any;

  const designer = useProcessDesigner({
    definitionId,
    instanceId,
    initialName: search.name,
    initialKey: search.key,
  });

  const {
    nodes, edges,
    selectedNode, selectedEdge,
    processName, processKey,
    reactFlowInstance, setReactFlowInstance,
    componentsOpened, openComponents, closeComponents,
    spotlightOpened, closeSpotlight,
    checklistOpened, closeChecklist,
    clearCanvasOpened, closeClearCanvas, confirmClearCanvas,
    lastSaved, isAutosaving,
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
    proceedWithSave, onSave, onExport, onImport,
    handleFileChange,
    undo, redo,
  } = designer;

  // All state and handlers come from the hook — no duplicate logic here.


  return (
    <Box h="calc(100vh - 60px)" style={{ display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
      <Box
        p="md"
        style={{
          backgroundColor: 'light-dark(var(--mantine-color-white), var(--mantine-color-dark-7))',
          borderBottom: '1px solid light-dark(var(--mantine-color-gray-2), var(--mantine-color-dark-4))',
        }}
      >
        <Group justify="space-between" align="center">
          <Stack gap={0}>
            <Title order={3} fw={800}>{processName || 'Process Designer'}</Title>
            <Group gap="xs">
              <Text size="xs" c="dimmed">Key: {processKey} • Status: Drafting</Text>
              {isAutosaving ? (
                <Badge variant="dot" color="blue" size="xs">Autosaving...</Badge>
              ) : lastSaved ? (
                <Text size="xs" c="dimmed">Last saved: {lastSaved.toLocaleTimeString()}</Text>
              ) : null}
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
              Components
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
            <Button 
              variant="filled" 
              color="indigo" 
              size="xs"
              leftSection={<Save size={14} />}
              onClick={onSave}
              loading={createDefinition.isPending}
            >
              Deploy Model
            </Button>
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
          <MiniMap 
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

                <Tooltip label="Clear Canvas">
                  <ActionIcon aria-label="Delete" variant="subtle" color="red" size="lg" onClick={clearCanvas}>
                    <Trash size={18} />
                  </ActionIcon>
                </Tooltip>
              </Group>
            </Paper>
          </Panel>

          {issues.length > 0 && (
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
          edges={edges}
          instanceId={instanceId}
          onViewInstance={onViewInstance}
        />
      </Box>

      <DesignerModals
        checklistOpened={checklistOpened}
        closeChecklist={closeChecklist}
        issues={issues}
        proceedWithSave={proceedWithSave}
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
