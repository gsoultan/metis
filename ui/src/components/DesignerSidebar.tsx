import React, { useState } from 'react';
import { Stack, Text, Paper, ThemeIcon, Group, Tooltip, Box, Divider, TextInput, Accordion, ActionIcon, UnstyledButton, Loader } from '@mantine/core';
import { 
  Play, 
  Square, 
  User, 
  Settings, 
  FileCode, 
  GitBranch, 
  Plus,
  Circle,
  Clock,
  Bell,
  ExternalLink,
  Hand,
  Briefcase,
  Search,
  X,
  GripVertical,
  Zap
} from 'lucide-react';
import classes from './DesignerSidebar.module.css';
import { useConnectors } from '../hooks/useProcess';

const designerItems = [
  { group: 'Events', items: [
    { type: 'startEvent', label: 'Start Event', description: 'The starting point of a process flow.', icon: Play, color: 'green' },
    { type: 'endEvent', label: 'End Event', description: 'The completion point of a process flow.', icon: Square, color: 'red' },
    { type: 'terminateEndEvent', label: 'Terminate Event', description: 'Immediately terminates all paths in the process.', icon: Zap, color: 'red' },
    { type: 'intermediateCatchEvent', label: 'Timer Event', description: 'Wait for a specific duration or date.', icon: Clock, color: 'blue', data: { icon: 'timer' } },
    { type: 'intermediateCatchEvent', label: 'Signal Event', description: 'Wait for a specific signal or message.', icon: Bell, color: 'blue', data: { icon: 'signal' } },
    { type: 'boundaryEvent', label: 'Boundary Event', description: 'An event attached to an activity boundary.', icon: Circle, color: 'orange', data: { icon: 'timer' } },
  ]},
  { group: 'Tasks', items: [
    { type: 'userTask', label: 'User Task', description: 'A task that must be completed by a person.', icon: User, color: 'blue' },
    { type: 'serviceTask', label: 'Service Task', description: 'An automated task performed by a service.', icon: Settings, color: 'teal' },
    { type: 'scriptTask', label: 'Script Task', description: 'Execute a custom script or expression.', icon: FileCode, color: 'violet' },
    { type: 'manualTask', label: 'Manual Task', description: 'A task performed manually without engine aid.', icon: Hand, color: 'orange' },
    { type: 'businessRuleTask', label: 'Business Rule', description: 'Execute a business decision or rule.', icon: Briefcase, color: 'indigo' },
    { type: 'callActivity', label: 'Call Activity', description: 'Invoke another process as a sub-process.', icon: ExternalLink, color: 'cyan' },
  ]},
  { group: 'Gateways', items: [
    { type: 'exclusiveGateway', label: 'Exclusive Gateway', description: 'Route to exactly one path based on conditions.', icon: GitBranch, color: 'orange' },
    { type: 'parallelGateway', label: 'Parallel Gateway', description: 'Split into multiple paths or synchronize them.', icon: Plus, color: 'orange' },
    { type: 'inclusiveGateway', label: 'Inclusive Gateway', description: 'Route to one or more paths based on conditions.', icon: Circle, color: 'orange' },
    { type: 'eventBasedGateway', label: 'Event Gateway', description: 'Wait for the first of multiple events to occur.', icon: Zap, color: 'orange' },
  ]},
  { group: 'Containers', items: [
    { type: 'subProcess', label: 'Sub Process', description: 'A container for a sub-process flow.', icon: Plus, color: 'indigo' },
    { type: 'pool', label: 'Pool', description: 'A container for a participant or organization.', icon: GripVertical, color: 'gray' },
    { type: 'lane', label: 'Lane', description: 'A sub-partition within a pool.', icon: GripVertical, color: 'gray' },
  ]},
];

interface DesignerSidebarProps {
  embedded?: boolean;
}

export function DesignerSidebar({ embedded }: DesignerSidebarProps) {
  const [search, setSearch] = useState('');
  const { data: connectorsData, isLoading: connectorsLoading } = useConnectors();
  
  const onDragStart = (event: React.DragEvent, nodeType: string, initialData: any = {}) => {
    event.dataTransfer.setData('application/reactflow', nodeType);
    event.dataTransfer.setData('application/initialData', JSON.stringify(initialData));
    event.dataTransfer.effectAllowed = 'move';
  };

  const connectors = (connectorsData as any)?.connectors || [];
  
  const connectorItems = connectors.map((c: any) => ({
    type: 'serviceTask',
    label: c.name,
    description: c.description,
    icon: Zap,
    color: 'yellow',
    data: {
      label: c.name,
      implementation: 'connector',
      connector_id: c.id,
    }
  }));

  const allGroups = [
    ...designerItems,
    ...(connectorItems.length > 0 ? [{ group: 'Connectors', items: connectorItems }] : [])
  ];

  const filteredItems = allGroups.map(group => ({
    ...group,
    items: group.items.filter((item: any) => 
      item.label.toLowerCase().includes(search.toLowerCase())
    )
  })).filter(group => group.items.length > 0);

  const content = (
    <Stack gap="md" style={{ height: '100%' }}>
      {!embedded && (
        <Box>
          <Text fw={800} size="lg" mb={4}>Components</Text>
          <Text size="xs" c="dimmed">Drag items to the canvas to build your process</Text>
        </Box>
      )}
      
      <TextInput
        placeholder="Search components..."
        size="xs"
        leftSection={<Search size={14} />}
        value={search}
        onChange={(e) => setSearch(e.currentTarget.value)}
        rightSection={
          search && (
            <ActionIcon aria-label="Remove" size="xs" variant="transparent" onClick={() => setSearch('')}>
              <X size={12} />
            </ActionIcon>
          )
        }
      />
      
      <Divider />
      
      <Box className={classes.scrollArea}>
        {search ? (
          <Stack gap="xs">
            {filteredItems.flatMap(group => group.items).map((item) => (
              <DesignerItem key={item.label} item={item} onDragStart={onDragStart} />
            ))}
            {filteredItems.length === 0 && (
              <Box py="xl" style={{ textAlign: 'center' }}>
                <Text size="sm" c="dimmed">No components found</Text>
              </Box>
            )}
          </Stack>
        ) : (
          <Accordion 
            multiple 
            defaultValue={['Events', 'Tasks', 'Gateways', 'Connectors']}
            variant="separated"
            classNames={{
              item: classes.accordionItem,
              control: classes.accordionControl,
              content: classes.accordionContent,
              label: classes.accordionLabel
            }}
          >
            {allGroups.map((group) => (
              <Accordion.Item key={group.group} value={group.group}>
                <Accordion.Control>
                  <Group gap="xs">
                    {group.group === 'Connectors' && connectorsLoading && <Loader size={12} />}
                    <Text size="sm" fw={600}>{group.group}</Text>
                  </Group>
                </Accordion.Control>
                <Accordion.Panel>
                  <Stack gap="xs">
                    {group.items.map((item: any) => (
                      <DesignerItem key={item.label} item={item} onDragStart={onDragStart} />
                    ))}
                  </Stack>
                </Accordion.Panel>
              </Accordion.Item>
            ))}
          </Accordion>
        )}
      </Box>
    </Stack>
  );

  if (embedded) {
    return (
      <Box className={classes.sidebar}>
        {content}
      </Box>
    );
  }

  return (
    <Paper 
      p="md" 
      withBorder 
      className={classes.sidebar}
      radius="lg"
      shadow="md"
    >
      {content}
    </Paper>
  );
}

function DesignerItem({ item, onDragStart }: { item: any, onDragStart: any }) {
  return (
    <Tooltip 
      label={`Drag to add ${item.label}`} 
      position="right" 
      withArrow
      openDelay={500}
    >
      <UnstyledButton
        component="div"
        draggable
        onDragStart={(event) => onDragStart(event, item.type, item.data)}
        className={classes.item}
      >
        <Group gap="sm" wrap="nowrap" align="center">
          <GripVertical size={14} color="var(--mantine-color-dimmed)" />
          <ThemeIcon color={item.color} variant="light" radius="md" size="md">
            <item.icon size={16} />
          </ThemeIcon>
          <Box style={{ flex: 1, minWidth: 0 }}>
            <Text size="sm" fw={600} truncate>{item.label}</Text>
            <Text size="xs" c="dimmed">{item.description}</Text>
          </Box>
        </Group>
      </UnstyledButton>
    </Tooltip>
  );
}
