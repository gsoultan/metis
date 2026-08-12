import { Badge, type MantineColor } from '@mantine/core';

interface StatusBadgeProps {
  status: string;
}

export function StatusBadge({ status }: StatusBadgeProps) {
  let color: MantineColor = 'gray';
  const label = status.toUpperCase();

  switch (status.toLowerCase()) {
    case 'running':
    case 'active':
    case 'in_progress':
      color = 'blue';
      break;
    case 'completed':
    case 'done':
    case 'finished':
      color = 'green';
      break;
    case 'failed':
    case 'error':
      color = 'red';
      break;
    case 'pending':
    case 'todo':
      color = 'yellow';
      break;
    case 'waiting':
      color = 'orange';
      break;
  }

  return (
    <Badge 
      variant="filled" 
      color={color} 
      radius="sm" 
      style={{ 
        height: 32, 
        minWidth: 100,
        fontWeight: 600,
        textTransform: 'capitalize',
        fontSize: 'var(--mantine-font-size-xs)'
      }}
    >
      {label}
    </Badge>
  );
}
