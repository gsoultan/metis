/**
 * The settings most people never touch, kept out of the way until they do.
 *
 * Lock durations, retry counts and idempotency keys all matter — and all have a
 * right default. Showing six of them beside "Start" turns a two-field step into
 * a form, and the person this screen is for has not yet learned which six they
 * can ignore.
 */
import { Button, Collapse, Stack } from '@mantine/core';
import { ChevronDown, ChevronRight } from 'lucide-react';
import { useId, useState } from 'react';
import type { ReactNode } from 'react';

interface SdkAdvancedProps {
  label?: string;
  children: ReactNode;
}

export function SdkAdvanced({ label = 'Advanced', children }: SdkAdvancedProps) {
  const [open, setOpen] = useState(false);
  const regionId = useId();

  return (
    <Stack gap={6}>
      <Button
        size="compact-xs"
        variant="subtle"
        color="gray"
        onClick={() => setOpen((current) => !current)}
        leftSection={open ? <ChevronDown size={13} /> : <ChevronRight size={13} />}
        aria-expanded={open}
        aria-controls={regionId}
        style={{ alignSelf: 'flex-start' }}
      >
        {label}
      </Button>
      <Collapse expanded={open}>
        <div id={regionId}>{children}</div>
      </Collapse>
    </Stack>
  );
}
