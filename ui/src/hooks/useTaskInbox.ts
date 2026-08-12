import { useState, useEffect, useMemo, useCallback } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { 
  useTasksByAssignee, 
  useTasksByCandidates,
  useTasks,
  useCompleteTask,
  useClaimTask,
  useUnclaimTask,
  useUpdateTask,
  useAssignTask,
  useUsers,
  useUserGroups,
} from './useProcess';
import { useAppStore } from '../store/useAppStore';

export function useTaskInbox() {
  const { currentOrganizationId, user } = useAppStore();
  const [currentUser, setCurrentUser] = useState(user?.username || 'manager');
  
  const { data: userGroupsData } = useUserGroups(user?.id || null);
  const userGroups = useMemo(() => {
    if (!userGroupsData?.groups) return [];
    return userGroupsData.groups
      .filter((g) => g.organization_id === currentOrganizationId)
      .map((g) => g.name);
  }, [userGroupsData, currentOrganizationId]);
  
  const [activeTab, setActiveTab] = useState<string | null>('assigned');
  const [viewMode, setViewMode] = useState<'table' | 'kanban'>('table');
  const [selectedTaskIds, setSelectedTaskIds] = useState<string[]>([]);
  const [searchQuery, setSearchQuery] = useState('');
  const [selectedTask, setSelectedTask] = useState<any | null>(null);
  const [editingTask, setEditingTask] = useState<any | null>(null);
  const [sortBy, setSortBy] = useState<string | null>('created_at');
  const [reverseSortDirection, setReverseSortDirection] = useState(true);
  
  const queryClient = useQueryClient();
  
  const { data: usersData } = useUsers(currentOrganizationId);
  const availableUsers = useMemo(() => 
    (usersData?.users || []).map((u) => ({ value: u.username, label: u.fullName || u.username })),
    [usersData]
  );
  
  const [reassignModalOpened, setReassignModalOpened] = useState(false);
  const [taskToReassign, setTaskToReassign] = useState<any>(null);
  const [newAssignee, setNewAssignee] = useState<string | null>(null);

  useEffect(() => {
    const eventSource = new EventSource('/api/v1/events');
    eventSource.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data);
        if (
          data.type === 'TaskCreated' || 
          data.type === 'TaskCompleted' || 
          data.type === 'TaskClaimed' || 
          data.type === 'TaskUpdated'
        ) {
          queryClient.invalidateQueries({ queryKey: ['tasks'] });
        }
      } catch (err) {
        console.error('Failed to parse SSE event:', err);
      }
    };
    return () => eventSource.close();
  }, [queryClient]);
  
  // Paging state lives here rather than in the page component, so the query
  // key and the controls cannot disagree about which page is showing.
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(25);

  const { data: assignedData, isLoading: assignedLoading } = useTasksByAssignee(currentUser, page, pageSize);

  // Any change to what is being filtered invalidates the current offset: page 4
  // of an unfiltered list is not page 4 of a filtered one, and landing on an
  // empty page reads as "no results" rather than "you are past the end".
  useEffect(() => {
    setPage(1);
  }, [currentUser, pageSize, activeTab]);
  const { data: candidateData, isLoading: candidateLoading } = useTasksByCandidates(currentUser, userGroups, page, pageSize);
  const { data: allTasksData, isLoading: allTasksLoading } = useTasks();
  
  const completeTaskMutation = useCompleteTask();
  const claimTaskMutation = useClaimTask();
  const unclaimTaskMutation = useUnclaimTask();
  const updateTaskMutation = useUpdateTask();
  const assignTaskMutation = useAssignTask();

  const assignedTasks = assignedData?.tasks || [];
  const candidateTasks = candidateData?.tasks || [];
  const allTasks = allTasksData?.tasks || [];
  // The count comes from the server's total, not from the rows on screen —
  // with paging those are different numbers, and the tab badge should say how
  // much work there is, not how much is currently rendered.
  const assignedPageInfo = assignedData?.pageInfo;
  const candidatePageInfo = candidateData?.pageInfo;
  const assignedCount = assignedPageInfo?.total ?? assignedTasks.length;
  const candidateCount = candidatePageInfo?.total ?? candidateTasks.length;

  const handleClaim = useCallback((id: string) => {
    claimTaskMutation.mutate({ id, userId: currentUser });
  }, [claimTaskMutation, currentUser]);

  const handleUnclaim = useCallback((id: string) => {
    unclaimTaskMutation.mutate(id);
  }, [unclaimTaskMutation]);

  const handleComplete = useCallback((id: string, variables: any) => {
    completeTaskMutation.mutate({ id, userId: currentUser, variables }, {
      onSuccess: () => setSelectedTask(null)
    });
  }, [completeTaskMutation, currentUser]);

  const handleAssign = useCallback((id: string, userId: string) => {
    assignTaskMutation.mutate({ id, userId }, {
      onSuccess: () => {
        setReassignModalOpened(false);
        setTaskToReassign(null);
        setNewAssignee(null);
      }
    });
  }, [assignTaskMutation]);

  const handleSort = useCallback((field: string) => {
    if (sortBy === field) {
      setReverseSortDirection(!reverseSortDirection);
    } else {
      setSortBy(field);
      setReverseSortDirection(false);
    }
  }, [sortBy, reverseSortDirection]);

  const filterAndSortTasks = useCallback((tasks: any[]) => {
    let filtered = [...tasks];
    if (searchQuery) {
      const query = searchQuery.toLowerCase();
      filtered = filtered.filter(t => 
        t.name?.toLowerCase().includes(query) || 
        t.description?.toLowerCase().includes(query) ||
        t.id?.toLowerCase().includes(query) ||
        t.instance_id?.toLowerCase().includes(query)
      );
    }

    if (sortBy) {
      filtered.sort((a, b) => {
        const aVal = a[sortBy];
        const bVal = b[sortBy];
        
        if (aVal === bVal) return 0;
        if (aVal === null || aVal === undefined) return 1;
        if (bVal === null || bVal === undefined) return -1;
        
        const result = aVal > bVal ? 1 : -1;
        return reverseSortDirection ? -result : result;
      });
    }
    return filtered;
  }, [searchQuery, sortBy, reverseSortDirection]);

  const currentTasks = useMemo(() => {
    const tasks = activeTab === 'assigned' ? assignedTasks : candidateTasks;
    return filterAndSortTasks(tasks);
  }, [activeTab, assignedTasks, candidateTasks, filterAndSortTasks]);

  const toggleSelection = useCallback((id: string) => {
    setSelectedTaskIds(prev => prev.includes(id) ? prev.filter(i => i !== id) : [...prev, id]);
  }, []);

  const handleBulkClaim = useCallback(() => {
    selectedTaskIds.forEach(id => claimTaskMutation.mutate({ id, userId: currentUser }));
    setSelectedTaskIds([]);
  }, [selectedTaskIds, claimTaskMutation, currentUser]);

  const handleBulkUnclaim = useCallback(() => {
    selectedTaskIds.forEach(id => unclaimTaskMutation.mutate(id));
    setSelectedTaskIds([]);
  }, [selectedTaskIds, unclaimTaskMutation]);

  return {
    currentUser,
    setCurrentUser,
    activeTab,
    setActiveTab,
    searchQuery,
    setSearchQuery,
    selectedTask,
    setSelectedTask,
    editingTask,
    setEditingTask,
    sortBy,
    handleSort,
    reverseSortDirection,
    availableUsers,
    reassignModalOpened,
    setReassignModalOpened,
    taskToReassign,
    setTaskToReassign,
    newAssignee,
    setNewAssignee,
    assignedLoading,
    candidateLoading,
    assignedCount,
    page,
    setPage,
    pageSize,
    setPageSize,
    assignedPageInfo,
    candidatePageInfo,
    // The page controls follow whichever tab is showing.
    activePageInfo: activeTab === 'assigned' ? assignedPageInfo : candidatePageInfo,
    candidateCount,
    currentTasks,
    viewMode,
    setViewMode,
    selectedTaskIds,
    setSelectedTaskIds,
    toggleSelection,
    handleBulkClaim,
    handleBulkUnclaim,
    allTasks,
    allTasksLoading,
    handleClaim,
    handleUnclaim,
    handleComplete,
    handleAssign,
    updateTaskMutation,
  };
}
