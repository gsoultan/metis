import {
  Modal,
  Stack,
  Text,
  Group,
  Paper,
  ThemeIcon,
  Badge,
  Divider,
  Button,
  Box,
  TextInput,
  Kbd,
  Drawer,
} from '@mantine/core';
import { AlertCircle, Save, Search, FileUp, Download, LayoutGrid, Undo2, Trash } from 'lucide-react';
import { DesignerSidebar } from './DesignerSidebar';
import type { ValidationIssue } from '../hooks/useProcessDesigner';

interface DesignerModalsProps {
  checklistOpened: boolean;
  closeChecklist: () => void;
  issues: ValidationIssue[];
  proceedWithSave: () => void;
  spotlightOpened: boolean;
  closeSpotlight: () => void;
  componentsOpened: boolean;
  closeComponents: () => void;
  /** clearCanvasOpened controls the Mantine confirm modal (replaces window.confirm). */
  clearCanvasOpened: boolean;
  closeClearCanvas: () => void;
  confirmClearCanvas: () => void;
  onImport: () => void;
  onExport: () => void;
  onSave: () => void;
  onAutoLayout: () => void;
  undo: () => void;
  clearCanvas: () => void;
}

export function DesignerModals({
  checklistOpened,
  closeChecklist,
  issues,
  proceedWithSave,
  spotlightOpened,
  closeSpotlight,
  componentsOpened,
  closeComponents,
  clearCanvasOpened,
  closeClearCanvas,
  confirmClearCanvas,
  onImport,
  onExport,
  onSave,
  onAutoLayout,
  undo,
  clearCanvas,
}: DesignerModalsProps) {
  return (
    <>
      <Modal
        opened={checklistOpened}
        onClose={closeChecklist}
        title={<Group gap="xs"><AlertCircle size={20} color="orange" /><Text fw={800}>Pre-deployment Checklist</Text></Group>}
        size="lg"
        radius="md"
      >
        <Stack gap="md">
          <Text size="sm" c="dimmed">We found some issues in your process model. Please review them before deploying.</Text>
          
          <Stack gap="sm">
            {issues.map((issue, idx) => (
              <Paper key={idx} withBorder p="sm" radius="md" bg={issue.severity === 'error' ? 'red.0' : 'orange.0'}>
                <Group justify="space-between" align="flex-start">
                  <Group gap="xs" style={{ flex: 1 }}>
                    <ThemeIcon size="sm" variant="transparent" color={issue.severity === 'error' ? 'red' : 'orange'}>
                      <AlertCircle size={16} />
                    </ThemeIcon>
                    <Stack gap={0}>
                      <Text size="sm" fw={700} c={issue.severity === 'error' ? 'red.9' : 'orange.9'}>{issue.message}</Text>
                      <Text size="xs" c="dimmed">{issue.suggestion}</Text>
                    </Stack>
                  </Group>
                  <Badge size="xs" color={issue.severity === 'error' ? 'red' : 'orange'}>{issue.severity}</Badge>
                </Group>
              </Paper>
            ))}
          </Stack>

          <Divider mt="md" />
          
          <Group justify="flex-end">
            <Button variant="default" onClick={closeChecklist}>Go Back to Editor</Button>
            <Button 
              color="indigo" 
              onClick={proceedWithSave} 
              disabled={issues.some(i => i.severity === 'error')}
              leftSection={<Save size={16} />}
            >
              Deploy Anyway
            </Button>
          </Group>
          {issues.some(i => i.severity === 'error') && (
            <Text size="xs" c="red" ta="right">Errors must be fixed before deployment.</Text>
          )}
        </Stack>
      </Modal>

      <Modal 
        opened={spotlightOpened} 
        onClose={closeSpotlight} 
        size="lg" 
        withCloseButton={false} 
        padding={0}
        radius="md"
        overlayProps={{ blur: 3, opacity: 0.55 }}
      >
        <Box p="md">
           <TextInput 
             placeholder="Search commands or elements..." 
             leftSection={<Search size={18} />} 
             size="md"
             variant="filled"
             autoFocus
             rightSection={<Kbd size="xs">Esc</Kbd>}
           />
           <Stack gap={4} mt="md">
              <Text size="xs" fw={700} c="dimmed" tt="uppercase" ml="xs">Actions</Text>
              <Button variant="subtle" justify="flex-start" leftSection={<FileUp size={16} />} color="gray" fullWidth onClick={() => { onImport(); closeSpotlight(); }}>
                Import BPMN Model <Kbd ml="auto" size="xs">Ctrl+I</Kbd>
              </Button>
              <Button variant="subtle" justify="flex-start" leftSection={<Download size={16} />} color="gray" fullWidth onClick={() => { onExport(); closeSpotlight(); }}>
                Export BPMN Model <Kbd ml="auto" size="xs">Ctrl+E</Kbd>
              </Button>
              <Button variant="subtle" justify="flex-start" leftSection={<Save size={16} />} color="gray" fullWidth onClick={() => { onSave(); closeSpotlight(); }}>
                Deploy Process Model <Kbd ml="auto" size="xs">Ctrl+S</Kbd>
              </Button>
              <Button variant="subtle" justify="flex-start" leftSection={<LayoutGrid size={16} />} color="gray" fullWidth onClick={() => { onAutoLayout(); closeSpotlight(); }}>
                Apply Auto-Layout
              </Button>
              <Button variant="subtle" justify="flex-start" leftSection={<Undo2 size={16} />} color="gray" fullWidth onClick={() => { undo(); closeSpotlight(); }}>
                Undo Last Action <Kbd ml="auto" size="xs">Ctrl+Z</Kbd>
              </Button>
              <Button variant="subtle" justify="flex-start" leftSection={<Trash size={16} />} color="red" fullWidth onClick={() => { clearCanvas(); closeSpotlight(); }}>
                Clear Canvas
              </Button>
           </Stack>
        </Box>
      </Modal>

      {/* FE-ARCH-7: Mantine modal replaces native window.confirm for clearing the canvas */}
      <Modal
        opened={clearCanvasOpened}
        onClose={closeClearCanvas}
        title={<Group gap="xs"><Trash size={18} color="red" /><Text fw={800}>Clear Canvas</Text></Group>}
        size="sm"
        radius="md"
      >
        <Stack gap="md">
          <Text size="sm">Are you sure you want to clear the entire canvas? This action cannot be undone.</Text>
          <Group justify="flex-end">
            <Button variant="default" onClick={closeClearCanvas}>Cancel</Button>
            <Button color="red" leftSection={<Trash size={16} />} onClick={confirmClearCanvas}>Clear</Button>
          </Group>
        </Stack>
      </Modal>

      <Drawer
        opened={componentsOpened}
        onClose={closeComponents}
        position="right"
        size="320px"
        title="Components"
        withOverlay={false}
        trapFocus={false}
        lockScroll={false}
        styles={{
          header: { fontWeight: 800 },
          body: { height: 'calc(100% - 60px)', padding: 16 }
        }}
      >
        <DesignerSidebar embedded />
      </Drawer>
    </>
  );
}
