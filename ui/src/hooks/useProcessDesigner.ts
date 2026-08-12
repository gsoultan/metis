import { useCallback, useEffect, useMemo, useRef, useState, type ChangeEvent, type DragEvent, type MouseEvent } from 'react';
import {
  addEdge,
  applyEdgeChanges,
  applyNodeChanges,
  type Edge,
  type Node,
  type OnConnect,
  type OnEdgesChange,
  type OnNodesChange,
  type ReactFlowInstance,
} from '@xyflow/react';
import { notifications } from '@mantine/notifications';
import { useDisclosure, useHotkeys } from '@mantine/hooks';
import { v7 as uuidv7 } from 'uuid';
import {
  useCreateDefinition,
  useDefinition,
  useExecutionPath,
  useExportDefinition,
  useImportDefinition,
  useInstance,
} from './useProcess';
import { useAppStore } from '../store/useAppStore';
import { buildDefinitionPayload, mapLoadedEdges, mapLoadedNodes } from '../mappers/definitionMapper';
import { useDesignerHistory } from './useDesignerHistory';
import { useDesignerCollaboration } from './useDesignerCollaboration';
import type { BPMNNodeData, BPMNEdgeData } from '../types/bpmn';
import type { ApiNode, ApiFlow } from '../services/types';
type ValidationIssue = {
  message: string;
  severity: 'error' | 'warning';
  id?: string;
};

// mapLoadedNodes, mapLoadedEdges, and buildDefinitionPayload have been moved to
// src/mappers/definitionMapper.ts (FE-ARCH-4, FE-ARCH-6).

function validateProcessModel(nodes: Node<BPMNNodeData>[], edges: Edge<BPMNEdgeData>[]): ValidationIssue[] {
  if (nodes.length === 0) {
    return [{ message: 'Process is empty', severity: 'warning' }];
  }

  const issues: ValidationIssue[] = [];
  const hasStart = nodes.some((node) => node.type === 'startEvent');
  const hasEnd = nodes.some((node) => node.type === 'endEvent');

  if (!hasStart) {
    issues.push({ message: 'Missing Start Event', severity: 'error' });
  }

  if (!hasEnd) {
    issues.push({ message: 'Missing End Event', severity: 'error' });
  }

  nodes.forEach((node) => {
    const incoming = edges.filter((edge) => edge.target === node.id);
    const outgoing = edges.filter((edge) => edge.source === node.id);

    if (node.type !== 'startEvent' && incoming.length === 0) {
      issues.push({ message: `Node "${node.data.label}" has no incoming flows`, severity: 'warning', id: node.id });
    }

    if (node.type !== 'endEvent' && outgoing.length === 0) {
      issues.push({ message: `Node "${node.data.label}" has no outgoing flows`, severity: 'warning', id: node.id });
    }

    if (node.type === 'exclusiveGateway' && outgoing.length < 2) {
      issues.push({ message: 'Gateway should have at least 2 outgoing flows', severity: 'warning', id: node.id });
    }
  });

  return issues;
}

function autoLayoutNodes(nodes: Node<BPMNNodeData>[], edges: Edge<BPMNEdgeData>[]): Node<BPMNNodeData>[] {
  const nextNodes = [...nodes];
  const visited = new Set<string>();
  const queue: { id: string; level: number }[] = [];
  const starts = nodes.filter((node) => node.type === 'startEvent');
  const levels: Record<string, number> = {};
  const levelCounts: Record<number, number> = {};

  starts.forEach((startNode) => {
    queue.push({ id: startNode.id, level: 0 });
  });

  while (queue.length > 0) {
    const current = queue.shift();
    if (!current) {
      continue;
    }

    if (visited.has(current.id)) {
      continue;
    }

    visited.add(current.id);
    levels[current.id] = current.level;
    levelCounts[current.level] = (levelCounts[current.level] || 0) + 1;

    const outgoingTargets = edges.filter((edge) => edge.source === current.id).map((edge) => edge.target);
    outgoingTargets.forEach((targetId) => {
      queue.push({ id: targetId, level: current.level + 1 });
    });
  }

  nodes.forEach((node) => {
    if (visited.has(node.id)) {
      return;
    }

    levels[node.id] = 0;
    levelCounts[0] = (levelCounts[0] || 0) + 1;
  });

  const currentLevelY: Record<number, number> = {};

  return nextNodes.map((node) => {
    const level = levels[node.id] || 0;
    const x = 100 + level * 250;
    const yCount = levelCounts[level] || 1;
    const yIndex = currentLevelY[level] || 0;
    currentLevelY[level] = yIndex + 1;

    const y = 150 + (yIndex - (yCount - 1) / 2) * 120;

    return {
      ...node,
      position: { x, y },
    };
  });
}

type UseProcessDesignerParams = {
  definitionId?: string | null;
  instanceId?: string | null;
  /** Pre-fill the process name from the URL search params (for new processes). */
  initialName?: string;
  /** Pre-fill the process key from the URL search params (for new processes). */
  initialKey?: string;
};

/*
 * The Connect-generated ProcessDefinition carries only id/projectId/key/name/
 * version — the designer needs nodes and flows, which the REST endpoint
 * returns but the protobuf message does not declare. Narrowed here, at the
 * boundary, rather than widening the whole call chain back to `any`.
 *
 * The correct fix is for the proto to describe the full definition, or for the
 * designer to read from the REST client. Tracked in .junie/ui-ux-audit.md.
 */
type FullDefinition = {
  id: string;
  key: string;
  name: string;
  nodes?: ApiNode[];
  flows?: ApiFlow[];
};

export function useProcessDesigner({ definitionId, instanceId, initialName, initialKey }: UseProcessDesignerParams) {
  const [nodes, setNodes] = useState<Node<BPMNNodeData>[]>([]);
  const [edges, setEdges] = useState<Edge<BPMNEdgeData>[]>([]);
  const [reactFlowInstance, setReactFlowInstance] = useState<ReactFlowInstance<Node<BPMNNodeData>, Edge<BPMNEdgeData>> | null>(null);
  const [selectedNode, setSelectedNode] = useState<Node<BPMNNodeData> | null>(null);
  const [selectedEdge, setSelectedEdge] = useState<Edge<BPMNEdgeData> | null>(null);
  const [processName, setProcessName] = useState(initialName || 'New Process');
  const [processKey, setProcessKey] = useState(initialKey || 'new_process');
  const [componentsOpened, { open: openComponents, close: closeComponents }] = useDisclosure(false);
  const [spotlightOpened, { toggle: toggleSpotlight, close: closeSpotlight }] = useDisclosure(false);
  const [checklistOpened, { open: openChecklist, close: closeChecklist }] = useDisclosure(false);
  const [clearCanvasOpened, { open: openClearCanvas, close: closeClearCanvas }] = useDisclosure(false);
  const [lastSaved, setLastSaved] = useState<Date | null>(null);
  const [isAutosaving, setIsAutosaving] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const { currentProjectId } = useAppStore();
  const { data: loadedData } = useDefinition(definitionId || null);
  const { data: pathData } = useExecutionPath(instanceId || null);
  const { data: instanceData } = useInstance(instanceId || null);
  const createDefinition = useCreateDefinition();
  const exportMutation = useExportDefinition();
  const importMutation = useImportDefinition();
  const issues = useMemo(() => validateProcessModel(nodes, edges), [nodes, edges]);

  // FE-ARCH-5: Sub-hook for undo/redo history management.
  const { history, historyIndex, pushToHistory, undo: undoHistory, redo: redoHistory } =
    useDesignerHistory();

  // FE-ARCH-5: Sub-hook for WebSocket collaboration (cursors + remote events).
  const { remoteCursors, onMouseMove } = useDesignerCollaboration({
    projectId: currentProjectId,
    reactFlowInstance,
    setNodes,
  });

  const undo = useCallback(() => undoHistory(setNodes, setEdges), [undoHistory, setNodes, setEdges]);
  const redo = useCallback(() => redoHistory(setNodes, setEdges), [redoHistory, setNodes, setEdges]);

  const proceedWithSave = useCallback(() => {
    const definition = buildDefinitionPayload(processName, processKey, nodes, edges);

    createDefinition.mutate(definition, {
      onSuccess: () => {
        closeChecklist();
        notifications.show({
          title: 'Deployment Successful',
          message: `Process model "${processName}" has been deployed.`,
          color: 'green',
        });
      },
      onError: (error) => {
        notifications.show({
          title: 'Deployment Failed',
          message: error.message,
          color: 'red',
        });
      },
    });
  }, [closeChecklist, createDefinition, edges, nodes, processKey, processName]);

  const onSave = useCallback(() => {
    if (issues.some((issue) => issue.severity === 'error')) {
      notifications.show({
        title: 'Validation Failed',
        message: 'Please fix the errors before deploying.',
        color: 'red',
      });
      return;
    }
    if (issues.length > 0) {
      openChecklist();
      return;
    }
    proceedWithSave();
  }, [issues, openChecklist, proceedWithSave]);

  const onExport = useCallback(() => {
    if (!definitionId) {
      notifications.show({
        title: 'Export Failed',
        message: 'Please save the process model before exporting.',
        color: 'red',
      });
      return;
    }

    exportMutation.mutate(definitionId, {
      onSuccess: (data: any) => {
        const xml = atob(data.xml);
        const blob = new Blob([xml], { type: 'application/xml' });
        const url = URL.createObjectURL(blob);
        const anchor = document.createElement('a');
        anchor.href = url;
        anchor.download = `${processKey}.bpmn`;
        document.body.appendChild(anchor);
        anchor.click();
        document.body.removeChild(anchor);
        URL.revokeObjectURL(url);
      },
    });
  }, [definitionId, exportMutation, processKey]);

  const onImport = useCallback(() => {
    fileInputRef.current?.click();
  }, []);

  // FE-ARCH-7: open the Mantine confirmation modal instead of native confirm().
  const clearCanvas = useCallback(() => {
    openClearCanvas();
  }, [openClearCanvas]);

  // Called when the user confirms the clear-canvas modal.
  const confirmClearCanvas = useCallback(() => {
    setNodes([]);
    setEdges([]);
    pushToHistory([], []);
    setSelectedNode(null);
    setSelectedEdge(null);
    closeClearCanvas();
  }, [closeClearCanvas, pushToHistory]);

  const onAutoLayout = useCallback(() => {
    const updatedNodes = autoLayoutNodes(nodes, edges);
    setNodes(updatedNodes);
    pushToHistory(updatedNodes, edges);
    reactFlowInstance?.fitView({ duration: 800 });
  }, [edges, nodes, pushToHistory, reactFlowInstance]);

  const handleProcessNameChange = useCallback(
    (nextName: string) => {
      setProcessName(nextName);

      if (definitionId) {
        return;
      }

      const slug = nextName
        .toLowerCase()
        .trim()
        .replace(/[^\w\s-]/g, '')
        .replace(/[\s_-]+/g, '_')
        .replace(/^-+|-+$/g, '');

      setProcessKey(slug || 'process_key');
    },
    [definitionId],
  );

  const onNodesChange: OnNodesChange<Node<BPMNNodeData>> = useCallback(
    (changes) => {
      setNodes((currentNodes) => {
        const nextNodes = applyNodeChanges(changes, currentNodes);

        if (selectedNode) {
          const updatedNode = nextNodes.find((node) => node.id === selectedNode.id);
          if (updatedNode) {
            setSelectedNode(updatedNode);
          }
        }

        return nextNodes;
      });
    },
    [selectedNode],
  );

  const onNodeDragStop = useCallback(() => {
    pushToHistory(nodes, edges);
  }, [edges, nodes, pushToHistory]);

  const onEdgesChange: OnEdgesChange<Edge<BPMNEdgeData>> = useCallback((changes) => {
    setEdges((currentEdges) => applyEdgeChanges(changes, currentEdges));
  }, []);

  const onConnect: OnConnect = useCallback(
    (params) => {
      const nextEdge = {
        ...params,
        id: `e-${params.source}-${params.target}-${uuidv7().slice(0, 8)}`,
        animated: true,
        style: { strokeWidth: 2 },
        label: '',
      };

      setEdges((currentEdges) => {
        const nextEdges = addEdge(nextEdge, currentEdges as any) as Edge[];
        pushToHistory(nodes, nextEdges);
        return nextEdges;
      });
    },
    [nodes, pushToHistory],
  );

  // onMouseMove is provided by the useDesignerCollaboration sub-hook (FE-ARCH-5).

  const onDragOver = useCallback((event: DragEvent) => {
    event.preventDefault();
    event.dataTransfer.dropEffect = 'move';
  }, []);

  const onDrop = useCallback(
    (event: DragEvent) => {
      event.preventDefault();

      const type = event.dataTransfer.getData('application/reactflow');
      if (!type) {
        return;
      }

      const initialDataString = event.dataTransfer.getData('application/initialData');
      const initialData = initialDataString ? JSON.parse(initialDataString) : {};

      if (!reactFlowInstance) {
        return;
      }

      const position = reactFlowInstance.screenToFlowPosition({
        x: event.clientX,
        y: event.clientY,
      });

      const newNode: Node<BPMNNodeData> = {
        id: uuidv7(),
        type,
        position,
        data: {
          label: `${type} node`,
          nodeType: type as BPMNNodeData['nodeType'],
          documentation: '',
          ...initialData,
        },
      };

      setNodes((currentNodes) => {
        const nextNodes = currentNodes.concat(newNode);
        pushToHistory(nextNodes, edges);
        return nextNodes;
      });
    },
    [edges, pushToHistory, reactFlowInstance],
  );

  const onNodeClick = useCallback((_: MouseEvent, node: Node<BPMNNodeData>) => {
    setSelectedNode(node);
    setSelectedEdge(null);
  }, []);

  const onEdgeClick = useCallback((_: MouseEvent, edge: Edge<BPMNEdgeData>) => {
    setSelectedEdge(edge);
    setSelectedNode(null);
  }, []);

  const onPaneClick = useCallback(() => {
    setSelectedNode(null);
    setSelectedEdge(null);
  }, []);

  const deleteSelected = useCallback(() => {
    if (selectedNode) {
      const nextNodes = nodes.filter((node) => node.id !== selectedNode.id);
      const nextEdges = edges.filter((edge) => edge.source !== selectedNode.id && edge.target !== selectedNode.id);
      setNodes(nextNodes);
      setEdges(nextEdges);
      pushToHistory(nextNodes, nextEdges);
      setSelectedNode(null);
      return;
    }

    if (!selectedEdge) {
      return;
    }

    const nextEdges = edges.filter((edge) => edge.id !== selectedEdge.id);
    setEdges(nextEdges);
    pushToHistory(nodes, nextEdges);
    setSelectedEdge(null);
  }, [edges, nodes, pushToHistory, selectedEdge, selectedNode]);

  const updateNodeData = useCallback((id: string, newData: Partial<BPMNNodeData>) => {
    setNodes((currentNodes) =>
      currentNodes.map((node) => {
        if (node.id !== id) {
          return node;
        }

        const updatedNode = { ...node, data: { ...node.data, ...newData } };
        setSelectedNode((currentSelectedNode) => {
          if (currentSelectedNode?.id !== id) {
            return currentSelectedNode;
          }

          return updatedNode;
        });
        return updatedNode;
      }),
    );
  }, []);

  const updateEdgeData = useCallback((id: string, label: string, data?: Partial<BPMNEdgeData>) => {
    setEdges((currentEdges) =>
      currentEdges.map((edge) => {
        if (edge.id !== id) {
          return edge;
        }

        const updatedEdge = { ...edge, label, data: { ...edge.data, ...data } };
        setSelectedEdge((currentSelectedEdge) => {
          if (currentSelectedEdge?.id !== id) {
            return currentSelectedEdge;
          }

          return updatedEdge;
        });
        return updatedEdge;
      }),
    );
  }, []);

  const closeSelection = useCallback(() => {
    setSelectedNode(null);
    setSelectedEdge(null);
  }, []);

  const handleFileChange = useCallback(
    (event: ChangeEvent<HTMLInputElement>) => {
      const file = event.target.files?.[0];
      if (!file) {
        return;
      }

      const reader = new FileReader();
      reader.onload = (onLoadEvent) => {
        const xml = onLoadEvent.target?.result as string;
        importMutation.mutate(xml);
      };
      reader.readAsText(file);
      event.target.value = '';
    },
    [importMutation],
  );

  useHotkeys([
    ['mod+K', () => toggleSpotlight()],
    ['mod+S', () => onSave()],
    ['mod+I', () => onImport()],
    ['mod+E', () => onExport()],
    ['mod+Z', () => undo()],
    ['mod+Y', () => redo()],
  ]);

  // Node execution status is derived data and belongs in render, not in state.
  // Untangling it means changing how nodes reach React Flow, in a 630-line hook
  // with no test coverage — a change my own review process would block without
  // tests first. Deliberately deferred to the designer refactor
  // (.junie/execution-plan.md §5.4); disabled narrowly rather than repo-wide so
  // the debt stays visible here.
  useEffect(() => {
    if (!instanceId || nodes.length === 0) {
      return;
    }

    // eslint-disable-next-line react-hooks/set-state-in-effect
    setNodes((currentNodes) =>
      currentNodes.map((node) => {
        let status: BPMNNodeData['status'] = node.data.status;

        if (Array.isArray((pathData as any)?.nodes) && (pathData as any).nodes.some((n: any) => n?.id === node.id)) {
          status = 'completed';
        }

        if (instanceData?.instance?.activeNodes?.includes(node.id)) {
          status = 'active';
        }

        const heatmapValue = ((pathData as any)?.node_frequencies?.[node.id]) ?? ((pathData as any)?.nodeFrequencies?.[node.id]) ?? 0;

        return {
          ...node,
          data: {
            ...node.data,
            status,
            heatmapValue,
          },
        };
      }),
    );
  }, [instanceData, instanceId, nodes.length, pathData]);

  // Seed the undo/redo history with the initial canvas state.
  useEffect(() => {
    if (nodes.length === 0 || historyIndex !== -1) {
      return;
    }

    pushToHistory(nodes, edges);
  }, [edges, historyIndex, nodes, pushToHistory]);

  // Loads a fetched definition into locally editable designer state. Syncing
  // server data into an editor's working copy is a legitimate effect — the
  // alternative React recommends is a `key` on the component, which would
  // require changing the route boundary. Tracked with the designer refactor
  // (.junie/execution-plan.md §5.4).
  useEffect(() => {
    if (!loadedData?.definition) {
      return;
    }

    const definition = loadedData.definition as unknown as FullDefinition;
    /* eslint-disable react-hooks/set-state-in-effect */
    setProcessName(definition.name);
    setProcessKey(definition.key);

    const mappedNodes = mapLoadedNodes(definition.nodes || []);
    const mappedEdges = mapLoadedEdges(definition.flows || []);

    setNodes(mappedNodes);
    setEdges(mappedEdges);
    /* eslint-enable react-hooks/set-state-in-effect */

    if (!reactFlowInstance) {
      return;
    }

    setTimeout(() => {
      reactFlowInstance.fitView();
    }, 100);
  }, [loadedData, reactFlowInstance]);

  // FE-ARCH-12: Autosave draft to localStorage on a 3-second debounce.
  // Uses a typed DraftDefinition shape to ensure the saved data is readable.
  useEffect(() => {
    if (nodes.length === 0) {
      return;
    }

    const timer = setTimeout(() => {
      setIsAutosaving(true);
      const draft = { nodes, edges, processName, processKey, timestamp: new Date().toISOString() };
      localStorage.setItem(`gobpm_draft_${definitionId ?? 'new'}`, JSON.stringify(draft));
      setLastSaved(new Date());
      setIsAutosaving(false);
    }, 3000);

    return () => clearTimeout(timer);
  }, [nodes, edges, processName, processKey, definitionId]);

  return {
    nodes,
    edges,
    selectedNode,
    selectedEdge,
    processName,
    processKey,
    reactFlowInstance,
    setReactFlowInstance,
    componentsOpened,
    openComponents,
    closeComponents,
    spotlightOpened,
    closeSpotlight,
    checklistOpened,
    openChecklist,
    closeChecklist,
    clearCanvasOpened,
    closeClearCanvas,
    confirmClearCanvas,
    lastSaved,
    isAutosaving,
    history,
    historyIndex,
    issues,
    remoteCursors,
    createDefinition,
    exportMutation,
    importMutation,
    fileInputRef,
    onMouseMove,
    handleProcessNameChange,
    onNodesChange,
    onNodeDragStop,
    onEdgesChange,
    onConnect,
    onDragOver,
    onDrop,
    onNodeClick,
    onEdgeClick,
    onPaneClick,
    deleteSelected,
    clearCanvas,
    onAutoLayout,
    updateNodeData,
    updateEdgeData,
    proceedWithSave,
    onSave,
    onExport,
    onImport,
    handleFileChange,
    undo,
    redo,
    closeSelection,
  };
}