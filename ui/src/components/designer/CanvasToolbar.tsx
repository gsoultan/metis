import { ActionIcon, Divider, Group, Paper, Tooltip } from '@mantine/core';
import {
  LayoutGrid,
  Maximize,
  PanelLeftClose,
  PanelLeftOpen,
  Redo2,
  Trash,
  Undo2,
  ZoomIn,
  ZoomOut,
} from 'lucide-react';

interface CanvasToolbarProps {
  paletteOpen: boolean;
  onTogglePalette: () => void;
  canUndo: boolean;
  canRedo: boolean;
  undoSteps: number;
  onUndo: () => void;
  onRedo: () => void;
  onZoomIn: () => void;
  onZoomOut: () => void;
  onFit: () => void;
  onAutoLayout: () => void;
  onClear: () => void;
}

/**
 * The floating bar under the canvas.
 *
 * It opened with a "Selection mode" button that did nothing — there was no
 * other mode to switch to — and repeated the zoom buttons React Flow already
 * drew in the opposite corner. One set of controls now, and the first one
 * shows or hides the building blocks.
 */
export function CanvasToolbar(props: CanvasToolbarProps) {
  return (
    <Paper p={4} withBorder radius="xl" shadow="md" mb="md" bg="var(--mantine-color-body)">
      <Group gap={4} wrap="nowrap">
        <Tool
          label={props.paletteOpen ? 'Hide building blocks' : 'Show building blocks'}
          onClick={props.onTogglePalette}
          active={props.paletteOpen}
        >
          {props.paletteOpen ? <PanelLeftClose size={18} /> : <PanelLeftOpen size={18} />}
        </Tool>

        <Divider orientation="vertical" />

        <Tool
          label={props.canUndo ? `Undo (${props.undoSteps} ${props.undoSteps === 1 ? 'step' : 'steps'})` : 'Undo'}
          onClick={props.onUndo}
          disabled={!props.canUndo}
        >
          <Undo2 size={18} />
        </Tool>
        <Tool label="Redo" onClick={props.onRedo} disabled={!props.canRedo}>
          <Redo2 size={18} />
        </Tool>

        <Divider orientation="vertical" />

        <Tool label="Zoom in" onClick={props.onZoomIn}>
          <ZoomIn size={18} />
        </Tool>
        <Tool label="Zoom out" onClick={props.onZoomOut}>
          <ZoomOut size={18} />
        </Tool>
        <Tool label="Fit diagram to view" onClick={props.onFit}>
          <Maximize size={18} />
        </Tool>
        <Tool label="Tidy up the layout" onClick={props.onAutoLayout}>
          <LayoutGrid size={18} />
        </Tool>

        <Divider orientation="vertical" />

        {/* The accessible name and the tooltip say the same thing: a screen
            reader user and a sighted user are told the same action. */}
        <Tool label="Clear the canvas" onClick={props.onClear} color="red">
          <Trash size={18} />
        </Tool>
      </Group>
    </Paper>
  );
}

function Tool({
  label,
  onClick,
  disabled,
  active,
  color = 'gray',
  children,
}: {
  label: string;
  onClick: () => void;
  disabled?: boolean;
  active?: boolean;
  color?: string;
  children: React.ReactNode;
}) {
  return (
    <Tooltip label={label}>
      <ActionIcon
        aria-label={label}
        aria-pressed={active}
        variant={active ? 'light' : 'subtle'}
        color={active ? 'indigo' : color}
        size="lg"
        radius="xl"
        disabled={disabled}
        onClick={onClick}
      >
        {children}
      </ActionIcon>
    </Tooltip>
  );
}
