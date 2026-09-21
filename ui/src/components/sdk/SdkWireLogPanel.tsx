/**
 * Every call this sandbox made, with both bodies.
 *
 * The argument about a broken integration is always "I sent the right thing" /
 * "we never got it", and it is settled by the request and the reply side by
 * side. Copying the whole transcript is one button because the next thing that
 * happens after reading it is pasting it into an issue.
 */
import { Badge, Button, Code, Collapse, CopyButton, Group, Stack, Text } from '@mantine/core';
import { Check, ChevronDown, ChevronRight, Copy, ScrollText, Trash2 } from 'lucide-react';
import { useState } from 'react';

import { callTone, formatLatency, wireLogAsText, type WireCall } from '../../domain/sdkWireLog';
import classes from './SdkStep.module.css';

const TONE_COLOUR = { ok: 'teal', refused: 'yellow', failed: 'red' } as const;

interface SdkWireLogPanelProps {
  log: WireCall[];
  onClear: () => void;
}

export function SdkWireLogPanel({ log, onClear }: SdkWireLogPanelProps) {
  const [openId, setOpenId] = useState<string | null>(null);

  if (log.length === 0) {
    return (
      <Stack gap="sm" align="center" py="xl">
        <ScrollText size={28} aria-hidden style={{ opacity: 0.4 }} />
        <Text size="sm" c="dimmed" ta="center" maw={280}>
          Nothing sent yet. Every call this page makes is recorded here with what went out and
          what came back.
        </Text>
      </Stack>
    );
  }

  return (
    <Stack gap="xs">
      <Group justify="space-between" wrap="nowrap">
        <Text size="xs" c="dimmed">
          {log.length === 1 ? '1 call' : `${log.length} calls`}, newest first
        </Text>
        <Group gap={2} wrap="nowrap">
          <CopyButton value={wireLogAsText(log)} timeout={1500}>
            {({ copied, copy }) => (
              <Button
                size="compact-xs"
                variant="subtle"
                color={copied ? 'teal' : 'gray'}
                leftSection={copied ? <Check size={12} /> : <Copy size={12} />}
                onClick={copy}
              >
                {copied ? 'Copied' : 'Copy all'}
              </Button>
            )}
          </CopyButton>
          <Button
            size="compact-xs"
            variant="subtle"
            color="gray"
            leftSection={<Trash2 size={12} />}
            onClick={onClear}
          >
            Clear
          </Button>
        </Group>
      </Group>

      <Stack gap={2}>
        {log.map((call) => {
          const tone = callTone(call);
          const open = openId === call.id;
          return (
            <div key={call.id}>
              <button
                type="button"
                className={classes.wireRow}
                onClick={() => setOpenId(open ? null : call.id)}
                aria-expanded={open}
                aria-label={`${call.label}, ${call.status ?? 'no reply'}. Show the request and reply.`}
              >
                <Group gap="xs" wrap="nowrap" justify="space-between">
                  <Group gap={6} wrap="nowrap" style={{ minWidth: 0 }}>
                    {open ? <ChevronDown size={13} aria-hidden /> : <ChevronRight size={13} aria-hidden />}
                    <Badge size="xs" radius="sm" variant="light" color={TONE_COLOUR[tone]}>
                      {call.status ?? 'no reply'}
                    </Badge>
                    <Text size="xs" fw={500} truncate>
                      {call.label}
                    </Text>
                  </Group>
                  <Text size="xs" c="dimmed" style={{ whiteSpace: 'nowrap' }}>
                    {formatLatency(call.durationMs)}
                  </Text>
                </Group>
                <Text size="xs" c="dimmed" ff="monospace" pl={19} truncate>
                  {call.method} {call.path}
                </Text>
              </button>

              <Collapse expanded={open}>
                <Stack gap={6} pl={19} pb="xs">
                  {call.error !== undefined && (
                    <Text size="xs" c="red.7">
                      {call.error}
                    </Text>
                  )}
                  <BodyBlock title="Sent" value={call.requestBody} />
                  <BodyBlock title="Received" value={call.responseBody} />
                </Stack>
              </Collapse>
            </div>
          );
        })}
      </Stack>
    </Stack>
  );
}

function BodyBlock({ title, value }: { title: string; value: unknown }) {
  if (value === undefined) {
    return null;
  }
  return (
    <div>
      <Text size="xs" c="dimmed" mb={2}>
        {title}
      </Text>
      <Code block style={{ fontSize: '0.72rem', maxHeight: 220, overflow: 'auto' }}>
        {JSON.stringify(value, null, 2)}
      </Code>
    </div>
  );
}
