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
import { DECIDE_GROUP_KIND, buildDecideGroup } from '../domain/decideGroup';
import { edgeWithData } from '../domain/edgeCaption';
import { templateById } from '../domain/processTemplates';
import { nextDeployStep, type DeployMode } from '../domain/versionRollout';
import {
  useCreateDefinition,
  useDefinitionVersions,
  useDefinition,
  useExecutionPath,
  useExportDefinition,
  useImportDefinition,
  useInstance,
} from './useProcess';
import { useAppStore } from '../store/useAppStore';
import { buildDefinitionPayload, mapLoadedEdges, mapLoadedNodes, restoredNodes } from '../mappers/definitionMapper';
import { stepSchemasOf } from '../domain/connectorStep';
import { validateProcess } from '../domain/processValidation';
import {
  buildDraft,
  describeDraftAge,
  draftKey,
  isWorthSaving,
  parseDraft,
  shouldOfferDraft,
  type DesignerDraft,
} from '../domain/designerDraft';
import { useConnectors } from './useConnectors';
import { useDesignerHistory } from './useDesignerHistory';
import { useDesignerCollaboration } from './useDesignerCollaboration';
import type { BPMNNodeData, BPMNEdgeData } from '../types/bpmn';
import type { ApiNode, ApiFlow } from '../services/types';
import { fromBase64 } from '../services/shared/bytes';
// The issue shape lives beside the checks that produce it. Re-exported here
// because the designer page and the checklist modal import it from the hook.
export type { ValidationIssue } from '../domain/processValidation';

// mapLoadedNodes, mapLoadedEdges, and buildDefinitionPayload have been moved to
// src/mappers/definitionMapper.ts (FE-ARCH-4, FE-ARCH-6).

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
  /** A template id, which seeds the canvas. See domain/processTemplates.ts. */
  initialTemplate?: string;
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

export function useProcessDesigner({ definitionId, instanceId, initialName, initialKey, initialTemplate }: UseProcessDesignerParams) {
  /*
   * A template seeds the canvas once, as initial state rather than in an
   * effect. Seeding in an effect would fight the draft-restore path below,
   * which also writes nodes — and a template quietly overwriting recovered
   * work is worse than never offering templates at all.
   *
   * An unknown id yields an empty canvas, which is the same as no template.
   */
  const seed = useMemo(() => {
    const template = templateById(initialTemplate);
    if (!template) return { nodes: [], edges: [] };
    const built = template.build(uuidv7);
    return {
      nodes: built.nodes as unknown as Node<BPMNNodeData>[],
      edges: built.edges as unknown as Edge<BPMNEdgeData>[],
    };
    // Built once per template id; rebuilding would mint new ids under the author.
  }, [initialTemplate]);

  const [nodes, setNodes] = useState<Node<BPMNNodeData>[]>(seed.nodes);
  const [edges, setEdges] = useState<Edge<BPMNEdgeData>[]>(seed.edges);
  const [reactFlowInstance, setReactFlowInstance] = useState<ReactFlowInstance<Node<BPMNNodeData>, Edge<BPMNEdgeData>> | null>(null);
  const [selectedNode, setSelectedNode] = useState<Node<BPMNNodeData> | null>(null);
  const [selectedEdge, setSelectedEdge] = useState<Edge<BPMNEdgeData> | null>(null);
  const [processName, setProcessName] = useState(initialName || 'New Process');
  const [processKey, setProcessKey] = useState(initialKey || 'new_process');
  const [componentsOpened, { open: openComponents, close: closeComponents }] = useDisclosure(false);
  const [spotlightOpened, { toggle: toggleSpotlight, close: closeSpotlight }] = useDisclosure(false);
  const [checklistOpened, { open: openChecklist, close: closeChecklist }] = useDisclosure(false);
  const [clearCanvasOpened, { open: openClearCanvas, close: closeClearCanvas }] = useDisclosure(false);
  const [rolloutOpened, { open: openRollout, close: closeRollout }] = useDisclosure(false);
  const [lastSaved, setLastSaved] = useState<Date | null>(null);
  // A draft found in storage that is newer than what the server returned. Held
  // until the person says whether to restore it; nothing is applied behind
  // their back, because the draft may be a colleague's abandoned experiment.
  const [offeredDraft, setOfferedDraft] = useState<DesignerDraft | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const { currentProjectId } = useAppStore();
  const { data: loadedData } = useDefinition(definitionId || null);
  const { data: pathData } = useExecutionPath(instanceId || null);
  const { data: instanceData } = useInstance(instanceId || null);
  const createDefinition = useCreateDefinition();
  // What is already deployed under this key, so the deploy dialog can say which
  // version stays live and how much work is still running on it.
  const { data: versionData } = useDefinitionVersions(processKey || null);
  const exportMutation = useExportDefinition();
  const importMutation = useImportDefinition();
  // Which connectors ask a step for fields of its own, so a lookup with no
  // query is flagged on the step rather than refused by the server.
  const { data: connectorsData } = useConnectors();
  const stepSchemas = useMemo(() => stepSchemasOf(connectorsData?.connectors ?? []), [connectorsData]);
  const issues = useMemo(() => validateProcess(nodes, edges, stepSchemas), [nodes, edges, stepSchemas]);

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

  const versions = useMemo(() => versionData?.versions ?? [], [versionData]);

  /**
   * Deploys, either live or staged.
   *
   * The distinction only reaches the wire; what it means for work in flight is
   * the same either way, and the success message says so. Instances that are
   * already running finish on the version they started on — that is the engine's
   * rule, not a setting — so the only thing this decides is where the next
   * instance begins.
   *
   * The mode is required, and not a boolean, so that a button cannot hand this
   * its click event: "Deploy Anyway" did, the event was taken for `stage`, and
   * every deploy made past a warning was staged instead of live.
   */
  const proceedWithSave = useCallback((mode: DeployMode) => {
    const stage = mode === 'staged';
    const definition = buildDefinitionPayload(processName, processKey, nodes, edges);

    createDefinition.mutate({ definition, stage }, {
      onSuccess: (result) => {
        closeChecklist();
        closeRollout();
        // The draft has been published, so keeping it would offer the same work
        // back on the next open as though it were unsaved.
        try {
          localStorage.removeItem(draftKey(definitionId));
        } catch {
          // Storage being unavailable does not make the deploy less successful.
        }
        setOfferedDraft(null);
        const version = result?.version ? `v${result.version}` : 'This version';
        notifications.show({
          title: stage ? 'Staged' : 'Deployed',
          message: stage
            ? `${version} of "${processName}" is saved but not live. Promote it from Version history when you are ready.`
            : `${version} of "${processName}" is live. New instances run it; anything already running finishes on its own version.`,
          color: stage ? 'blue' : 'green',
        });
      },
      onError: (error) => {
        notifications.show({
          title: stage ? 'Could not stage this version' : 'Deployment Failed',
          message: error.message,
          color: 'red',
        });
      },
    });
  }, [closeChecklist, closeRollout, createDefinition, definitionId, edges, nodes, processKey, processName]);

  const onSave = useCallback(() => {
    switch (nextDeployStep(issues, versions)) {
      case 'held':
        // The checklist lists each problem and what to do about it; a toast that
        // only says "fix the errors" leaves the person hunting for them.
        openChecklist();
        notifications.show({
          title: 'This process would not run',
          message: 'Deploy is held until the problems listed are fixed.',
          color: 'red',
        });
        return;
      case 'review':
        openChecklist();
        return;
      case 'ask':
        // Only when there is a version that could stay live. The first deploy
        // of a process has no incumbent, so the question would have one answer.
        openRollout();
        return;
      case 'live':
        proceedWithSave('live');
    }
  }, [issues, openChecklist, openRollout, proceedWithSave, versions]);

  /**
   * Deploys past the checklist's warnings: on to whatever a deploy with none
   * would do next — the rollout question when a live version would be
   * replaced. It used to deploy on the spot, skipping that question.
   */
  const deployAnyway = useCallback(() => {
    closeChecklist();
    if (nextDeployStep([], versions) === 'ask') {
      openRollout();
      return;
    }
    proceedWithSave('live');
  }, [closeChecklist, openRollout, proceedWithSave, versions]);

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
      onSuccess: (data) => {
        // An export with no XML is a failed export; decoding undefined throws
        // and the download silently never starts.
        if (!data?.xml) {
          notifications.show({ title: 'Export failed', message: 'The server returned no BPMN XML', color: 'red' });
          return;
        }
        const xml = fromBase64(data.xml);
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
        const nextEdges = addEdge<Edge<BPMNEdgeData>>(nextEdge, currentEdges);
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

      // One palette item that lands two steps already wired — the recommended
      // way to route a process, made no harder than the unrecommended one.
      // See domain/decideGroup.ts.
      if (type === DECIDE_GROUP_KIND) {
        const group = buildDecideGroup(position, uuidv7);
        const groupNodes = group.nodes as unknown as Node<BPMNNodeData>[];
        const groupEdges = group.edges as unknown as Edge<BPMNEdgeData>[];

        setNodes((currentNodes) => {
          const nextNodes = currentNodes.concat(groupNodes);
          setEdges((currentEdges) => {
            const nextEdges = currentEdges.concat(groupEdges);
            pushToHistory(nextNodes, nextEdges);
            return nextEdges;
          });
          return nextNodes;
        });
        // The table is what the author has to choose; select it so the property
        // panel opens on the one decision they still have to make.
        setSelectedNode(groupNodes.find((node) => node.id === group.focusId) ?? null);
        return;
      }

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
    [edges, pushToHistory, reactFlowInstance, setEdges, setSelectedNode],
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

  const updateEdgeData = useCallback((id: string, data: Partial<BPMNEdgeData>) => {
    setEdges((currentEdges) =>
      currentEdges.map((edge) => {
        if (edge.id !== id) {
          return edge;
        }

        // The arrow's caption is the condition it carries, never a separate
        // string somebody typed: a sequence flow has no name on the server, so
        // a typed caption could not be saved, and the save mapper used to
        // deploy it as the condition instead. Deriving it here keeps the canvas
        // honest wherever the condition is edited from.
        const updatedEdge = edgeWithData(edge, data);
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
    ['mod+S', () => onSaveDraft()],
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

        if (pathData?.nodes?.some((n) => n?.id === node.id)) {
          status = 'completed';
        }

        // activeNodes carries Node messages now, not bare ids — the proto models a
        // relationship as an object. Comparing against the id keeps the check
        // working whichever shape a given server sends.
        if (instanceData?.instance?.activeNodes?.some((n) => (typeof n === 'string' ? n : n?.id) === node.id)) {
          status = 'active';
        }

        // The service normalises the protobuf nodeFrequencies to this name, so
        // there is one spelling to read rather than a guess at two.
        const heatmapValue = pathData?.node_frequencies?.[node.id] ?? 0;

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

  // Offer back anything this browser autosaved after the version that was
  // deployed. Runs once per definition; the answer is the person's to give.
  const draftChecked = useRef<string | null>(null);
  useEffect(() => {
    const key = draftKey(definitionId);
    if (draftChecked.current === key) {
      return;
    }
    // For a saved process, wait until the server's copy has arrived so the two
    // timestamps can be compared; a new process has nothing to wait for.
    if (definitionId && !loadedData?.definition) {
      return;
    }
    draftChecked.current = key;

    let stored: string | null = null;
    try {
      stored = localStorage.getItem(key);
    } catch {
      return;
    }
    const draft = parseDraft(stored);
    const deployedAt = (loadedData?.definition as unknown as { created_at?: string } | undefined)?.created_at;
    if (shouldOfferDraft(draft, deployedAt)) {
      /* eslint-disable-next-line react-hooks/set-state-in-effect */
      setOfferedDraft(draft);
    }
  }, [definitionId, loadedData]);

  // Autosave the working copy to this browser on a 3-second debounce.
  //
  // This is a crash net, not a save: the draft lives in localStorage and only
  // this browser can see it. It is offered back on open (see below) — for a
  // long time it was written and never read, so the header claimed "last saved"
  // while a closed tab threw the work away.
  const saveDraft = useCallback(() => {
    if (!isWorthSaving(nodes)) {
      return false;
    }
    const draft = buildDraft({ definitionId, processName, processKey, nodes, edges });
    try {
      localStorage.setItem(draftKey(definitionId), JSON.stringify(draft));
    } catch {
      // A full or unavailable storage must not break editing; the draft is a
      // convenience, and deploying is what actually saves.
      return false;
    }
    setLastSaved(new Date());
    return true;
  }, [definitionId, edges, nodes, processKey, processName]);

  useEffect(() => {
    if (!isWorthSaving(nodes)) {
      return;
    }
    const timer = setTimeout(saveDraft, 3000);
    return () => clearTimeout(timer);
  }, [nodes, edges, processName, processKey, saveDraft]);

  /** Ctrl/Cmd-S keeps the work; it does not publish it. */
  const onSaveDraft = useCallback(() => {
    if (saveDraft()) {
      notifications.show({
        title: 'Draft kept in this browser',
        message: 'Your changes are safe if this tab closes. Deploy when you want the process to run.',
        color: 'blue',
      });
    }
  }, [saveDraft]);

  /** Applies the draft the person chose to restore. */
  const restoreDraft = useCallback(() => {
    if (!offeredDraft) return;
    setNodes(restoredNodes(offeredDraft.nodes as typeof nodes));
    setEdges(offeredDraft.edges as typeof edges);
    if (offeredDraft.processName) setProcessName(offeredDraft.processName);
    if (offeredDraft.processKey) setProcessKey(offeredDraft.processKey);
    setOfferedDraft(null);
  }, [offeredDraft, setEdges, setNodes]);

  /** Throws the draft away and keeps what the server returned. */
  const discardDraft = useCallback(() => {
    try {
      localStorage.removeItem(draftKey(definitionId));
    } catch {
      // Nothing to do: the draft is already not being applied.
    }
    setOfferedDraft(null);
  }, [definitionId]);

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
    rolloutOpened,
    openRollout,
    closeRollout,
    versions,
    deploying: createDefinition.isPending,
    closeChecklist,
    clearCanvasOpened,
    closeClearCanvas,
    confirmClearCanvas,
    lastSaved,
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
    offeredDraft,
    offeredDraftAge: offeredDraft ? describeDraftAge(offeredDraft) : '',
    restoreDraft,
    discardDraft,
    onSaveDraft,
    proceedWithSave,
    deployAnyway,
    onSave,
    onExport,
    onImport,
    handleFileChange,
    undo,
    redo,
    closeSelection,
  };
}