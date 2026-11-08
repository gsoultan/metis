import { Badge, Button, Skeleton } from '@mantine/core';
import { Smartphone } from 'lucide-react';

interface DeviceCountProps {
  count: number;
  loading: boolean;
  onOpen: () => void;
}

/** How many phones a person registered for push, opening the list of them. */
export function DeviceCount({ count, loading, onOpen }: DeviceCountProps) {
  if (loading) return <Skeleton height={20} width={80} />;
  return (
    <Button variant="subtle" size="compact-xs" px={4} onClick={onOpen} leftSection={<Smartphone size={12} />}>
      <Badge size="sm" variant="light" color={count > 0 ? 'teal' : 'gray'} style={{ cursor: 'pointer' }}>
        {count === 0 ? 'None' : `${count} ${count === 1 ? 'device' : 'devices'}`}
      </Badge>
    </Button>
  );
}
