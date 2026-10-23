import { Text, UnstyledButton } from '@mantine/core';

import classes from './RowNameLink.module.css';

/**
 * A list row's name, which opens what the row stands for.
 *
 * The process and decision lists showed the name as plain text, and the only
 * way in was one of several coloured icons at the far end of the row.
 */
export function RowNameLink({ name, onOpen }: { name: string; onOpen: () => void }) {
  return (
    <UnstyledButton onClick={onOpen} className={classes.name}>
      <Text fw={700} size="sm" inherit truncate>
        {name}
      </Text>
    </UnstyledButton>
  );
}
