import { ActionIcon, Divider, Flex, Paper, Tooltip } from '@mantine/core';
import {
  LayoutGrid,
  Maximize,
  PanelLeftClose,
  PanelLeftOpen,
  Plus,
  Redo2,
  Trash,
  Undo2,
  ZoomIn,
  ZoomOut,
  X,
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
  /** A rail down the left of the canvas, rather than a bar under it. */
  vertical?: boolean;
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
  const vertical = props.vertical ?? false;
  const tooltipSide = vertical ? 'right' : 'top';
  return (
    <Paper p={4} withBorder radius="xl" shadow="md" mb={vertical ? 0 : 'md'} bg="var(--mantine-color-body)">
      <Flex gap={4} direction={vertical ? 'column' : 'row'} align="center" wrap="nowrap">
        {vertical ? (
          <Tooltip label={props.paletteOpen ? 'Hide building blocks' : 'Add a step'} position={tooltipSide}>
            <ActionIcon
              aria-label={props.paletteOpen ? 'Hide building blocks' : 'Add a step'}
              aria-pressed={props.paletteOpen}
              variant="filled"
              color="indigo"
              size="xl"
              radius="xl"
              onClick={props.onTogglePalette}
            >
              {props.paletteOpen ? <X size={20} /> : <Plus size={20} />}
            </ActionIcon>
          </Tooltip>
        ) : (
          <Tool
            side={tooltipSide}
            label={props.paletteOpen ? 'Hide building blocks' : 'Show building blocks'}
            onClick={props.onTogglePalette}
            active={props.paletteOpen}
          >
            {props.paletteOpen ? <PanelLeftClose size={18} /> : <PanelLeftOpen size={18} />}
          </Tool>
        )}

        <Divider orientation={vertical ? 'horizontal' : 'vertical'} w={vertical ? 24 : undefined} />

        <Tool
            side={tooltipSide}
          label={props.canUndo ? `Undo (${props.undoSteps} ${props.undoSteps === 1 ? 'step' : 'steps'})` : 'Undo'}
          onClick={props.onUndo}
          disabled={!props.canUndo}
        >
          <Undo2 size={18} />
        </Tool>
        <Tool side={tooltipSide} label="Redo" onClick={props.onRedo} disabled={!props.canRedo}>
          <Redo2 size={18} />
        </Tool>

        <Divider orientation={vertical ? 'horizontal' : 'vertical'} w={vertical ? 24 : undefined} />

        <Tool side={tooltipSide} label="Zoom in" onClick={props.onZoomIn}>
          <ZoomIn size={18} />
        </Tool>
        <Tool side={tooltipSide} label="Zoom out" onClick={props.onZoomOut}>
          <ZoomOut size={18} />
        </Tool>
        <Tool side={tooltipSide} label="Fit diagram to view" onClick={props.onFit}>
          <Maximize size={18} />
        </Tool>
        <Tool side={tooltipSide} label="Tidy up the layout" onClick={props.onAutoLayout}>
          <LayoutGrid size={18} />
        </Tool>

        <Divider orientation={vertical ? 'horizontal' : 'vertical'} w={vertical ? 24 : undefined} />

        {/* The accessible name and the tooltip say the same thing: a screen
            reader user and a sighted user are told the same action. */}
        <Tool side={tooltipSide} label="Clear the canvas" onClick={props.onClear} color="red">
          <Trash size={18} />
        </Tool>
      </Flex>
    </Paper>
  );
}

function Tool({
  side = 'top',
  label,
  onClick,
  disabled,
  active,
  color = 'gray',
  children,
}: {
  side?: 'top' | 'right';
  label: string;
  onClick: () => void;
  disabled?: boolean;
  active?: boolean;
  color?: string;
  children: React.ReactNode;
}) {
  return (
    <Tooltip label={label} position={side}>
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
