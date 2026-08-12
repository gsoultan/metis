import { 
  Stack, 
  TextInput, 
  Select, 
  Checkbox, 
  Button, 
  Group, 
  ActionIcon, 
  Text, 
  Divider, 
  Box,
  Paper,
  Tooltip,
  Tabs,
} from '@mantine/core';
import { Plus, Trash2, GripVertical, Settings2, EyeOff, Zap, LayoutPanelTop, Play } from 'lucide-react';
import { v4 as uuidv4 } from 'uuid';
import { VisualConditionBuilder } from './LowCodeComponents';
import { TaskForm } from './TaskForm';
import { useState } from 'react';

export interface FormField {
  id: string;
  label: string;
  type: 'text' | 'number' | 'date' | 'select' | 'boolean' | 'textarea' | 'section';
  placeholder?: string;
  description?: string;
  defaultValue?: any;
  required?: boolean;
  gridSpan?: number; // 1 or 2
  options?: { value: string; label: string }[];
  validation?: {
    pattern?: string;
    message?: string;
    customJs?: string;
  };
  logic?: {
    hiddenIf?: string;
    disabledIf?: string;
  };
  dataSource?: {
    type: 'static' | 'variable' | 'endpoint';
    variableKey?: string;
    endpointUrl?: string;
  };
}

interface FormBuilderProps {
  fields: FormField[];
  onChange: (fields: FormField[]) => void;
}

export function FormBuilder({ fields, onChange }: FormBuilderProps) {
  const [activeTab, setActiveTab] = useState<string>('editor');
  const [draggedIndex, setDraggedIndex] = useState<number | null>(null);

  const addField = () => {
    const newField: FormField = {
      id: `field_${uuidv4().substring(0, 8)}`,
      label: 'New Field',
      type: 'text',
      required: false,
    };
    onChange([...fields, newField]);
  };

  const removeField = (id: string) => {
    onChange(fields.filter(f => f.id !== id));
  };

  const updateField = (id: string, updates: Partial<FormField>) => {
    onChange(fields.map(f => f.id === id ? { ...f, ...updates } : f));
  };

  const moveUp = (index: number) => {
    if (index === 0) return;
    const next = [...fields];
    [next[index - 1], next[index]] = [next[index], next[index - 1]];
    onChange(next);
  };

  const moveDown = (index: number) => {
    if (index === fields.length - 1) return;
    const next = [...fields];
    [next[index + 1], next[index]] = [next[index], next[index + 1]];
    onChange(next);
  };

  const handleDragStart = (e: React.DragEvent, index: number) => {
    setDraggedIndex(index);
    e.dataTransfer.effectAllowed = 'move';
    // Create a ghost image if needed, but simple is better
  };

  const handleDragOver = (e: React.DragEvent, index: number) => {
    e.preventDefault();
    if (draggedIndex === null || draggedIndex === index) return;
    
    const next = [...fields];
    const item = next.splice(draggedIndex, 1)[0];
    next.splice(index, 0, item);
    setDraggedIndex(index);
    onChange(next);
  };

  const handleDragEnd = () => {
    setDraggedIndex(null);
  };

  return (
    <Stack gap="md">
      <Tabs value={activeTab} onChange={(val) => setActiveTab(val as string)} variant="pills" radius="md">
        <Tabs.List mb="lg">
          <Tabs.Tab value="editor" leftSection={<LayoutPanelTop size={14} />}>Designer</Tabs.Tab>
          <Tabs.Tab value="preview" leftSection={<Play size={14} />}>Live Preview</Tabs.Tab>
        </Tabs.List>

        <Tabs.Panel value="editor">
          <Stack gap="md">
            <Group justify="space-between">
              <Box>
                <Text fw={700} size="sm">Form Fields</Text>
                <Text size="xs" c="dimmed">Drag fields to reorder. Use tabs to configure logic and validation.</Text>
              </Box>
              <Button size="xs" variant="light" leftSection={<Plus size={14} />} onClick={addField}>
                Add Field
              </Button>
            </Group>

            {fields.length === 0 ? (
              <Paper withBorder p="xl" radius="md" bg="var(--mantine-color-gray-0)" style={{ borderStyle: 'dashed', textAlign: 'center' }}>
                <Text size="sm" c="dimmed">No fields defined yet. Add a field to start building your form.</Text>
              </Paper>
            ) : (
              <Stack gap="sm">
                {fields.map((field, index) => (
                  <div
                    key={field.id}
                    draggable
                    onDragStart={(e) => handleDragStart(e, index)}
                    onDragOver={(e) => handleDragOver(e, index)}
                    onDragEnd={handleDragEnd}
                    style={{ 
                      opacity: draggedIndex === index ? 0.5 : 1,
                      cursor: 'move',
                      transition: 'all 0.2s ease'
                    }}
                  >
                    <FieldEditor 
                      field={field} 
                      onUpdate={(updates) => updateField(field.id, updates)}
                      onRemove={() => removeField(field.id)}
                      onMoveUp={() => moveUp(index)}
                      onMoveDown={() => moveDown(index)}
                      isFirst={index === 0}
                      isLast={index === fields.length - 1}
                    />
                  </div>
                ))}
              </Stack>
            )}
          </Stack>
        </Tabs.Panel>

        <Tabs.Panel value="preview">
          <Paper withBorder p="xl" radius="lg" bg="gray.0">
            <Stack gap="xl">
              <Box>
                <Text fw={700} size="md">Form Preview</Text>
                <Text size="xs" c="dimmed">This is how the form will appear to the user.</Text>
              </Box>
              <Paper withBorder p="xl" radius="md" bg="white" shadow="sm">
                <TaskForm 
                  fields={fields} 
                  variables={{}} 
                  onSubmit={(vals) => console.log('Preview submit:', vals)} 
                />
              </Paper>
            </Stack>
          </Paper>
        </Tabs.Panel>
      </Tabs>
    </Stack>
  );
}

function FieldEditor({ 
  field, 
  onUpdate, 
  onRemove, 
  onMoveUp, 
  onMoveDown,
  isFirst,
  isLast
}: { 
  field: FormField, 
  onUpdate: (updates: Partial<FormField>) => void, 
  onRemove: () => void,
  onMoveUp: () => void,
  onMoveDown: () => void,
  isFirst: boolean,
  isLast: boolean
}) {
  return (
    <Paper withBorder p="md" radius="md" shadow="xs">
      <Stack gap="sm">
        <Group justify="space-between" wrap="nowrap">
          <Group gap="xs">
            <ActionIcon aria-label="Reorder field" variant="subtle" color="gray" style={{ cursor: 'default' }}>
              <GripVertical size={16} />
            </ActionIcon>
            <TextInput 
              variant="unstyled" 
              placeholder="Field Label" 
              value={field.label}
              onChange={(e) => onUpdate({ label: e.target.value })}
              styles={{ input: { fontWeight: 700, fontSize: '14px' } }}
            />
          </Group>
          <Group gap={4}>
            <Tooltip label="Move Up">
              <ActionIcon aria-label="Add field" variant="subtle" color="gray" size="sm" onClick={onMoveUp} disabled={isFirst}>
                <Plus size={14} style={{ transform: 'rotate(180deg)' }} />
              </ActionIcon>
            </Tooltip>
            <Tooltip label="Move Down">
              <ActionIcon aria-label="Add field" variant="subtle" color="gray" size="sm" onClick={onMoveDown} disabled={isLast}>
                <Plus size={14} />
              </ActionIcon>
            </Tooltip>
            <ActionIcon aria-label="Delete field" variant="light" color="red" size="sm" onClick={onRemove}>
              <Trash2 size={14} />
            </ActionIcon>
          </Group>
        </Group>

        <Divider variant="dashed" />

        <Tabs defaultValue="basic" variant="pills" radius="md">
          <Tabs.List>
            <Tabs.Tab value="basic" leftSection={<Settings2 size={12} />}>Basic</Tabs.Tab>
            <Tabs.Tab value="validation" leftSection={<Plus size={12} />}>Validation</Tabs.Tab>
            <Tabs.Tab value="logic" leftSection={<EyeOff size={12} />}>Logic</Tabs.Tab>
            <Tabs.Tab value="data" leftSection={<Zap size={12} />}>Data</Tabs.Tab>
          </Tabs.List>

          <Tabs.Panel value="basic" pt="xs">
            <Stack gap="xs">
              <Group grow align="flex-start">
                <TextInput 
                  label="Field ID (Variable Key)" 
                  size="xs"
                  placeholder="e.g. orderAmount"
                  description="The internal key for process variables"
                  value={field.id}
                  onChange={(e) => onUpdate({ id: e.target.value })}
                />
                <Select 
                  label="Type"
                  size="xs"
                  description="Data type of the field"
                  data={[
                    { value: 'text', label: 'Text' },
                    { value: 'textarea', label: 'Long Text' },
                    { value: 'number', label: 'Number' },
                    { value: 'date', label: 'Date' },
                    { value: 'select', label: 'Dropdown' },
                    { value: 'boolean', label: 'Checkbox/Switch' },
                    { value: 'section', label: 'Section/Group' },
                  ]}
                  value={field.type}
                  onChange={(val) => onUpdate({ type: val as any })}
                />
              </Group>

              <Group grow align="center">
                <TextInput 
                  label="Placeholder" 
                  size="xs"
                  placeholder="Helpful hint for the user"
                  description="Short hint inside the input"
                  value={field.placeholder || ''}
                  onChange={(e) => onUpdate({ placeholder: e.target.value })}
                />
                <TextInput 
                  label="Description" 
                  size="xs"
                  placeholder="Detailed instructions"
                  description="Helper text below the input"
                  value={field.description || ''}
                  onChange={(e) => onUpdate({ description: e.target.value })}
                />
              </Group>

              <Group grow align="center">
                <Select 
                  label="Column Span"
                  size="xs"
                  description="Grid width of the field"
                  data={[
                    { value: '1', label: '1 Column' },
                    { value: '2', label: '2 Columns (Full Width)' },
                  ]}
                  value={String(field.gridSpan || 1)}
                  onChange={(val) => onUpdate({ gridSpan: Number(val) })}
                />
                <Box pt="md">
                  <Checkbox 
                      label="Required" 
                      description="Validation mandatory"
                      size="xs" 
                      checked={field.required}
                      onChange={(e) => onUpdate({ required: e.target.checked })}
                  />
                </Box>
              </Group>

              {field.type === 'select' && (!field.dataSource || field.dataSource.type === 'static') && (
                <Box>
                  <Text size="xs" fw={700} mb="xs">Options</Text>
                  <Stack gap="xs">
                    {(field.options || []).map((opt, i) => (
                      <Group key={i} gap="xs" align="flex-start">
                        <TextInput 
                          label={i === 0 ? "Option Value" : undefined}
                          description={i === 0 ? "Unique ID" : undefined}
                          size="xs" 
                          placeholder="Value" 
                          style={{ flex: 1 }}
                          value={opt.value}
                          onChange={(e) => {
                            const next = [...(field.options || [])];
                            next[i] = { ...opt, value: e.target.value };
                            onUpdate({ options: next });
                          }}
                        />
                        <TextInput 
                          label={i === 0 ? "Option Label" : undefined}
                          description={i === 0 ? "Display text" : undefined}
                          size="xs" 
                          placeholder="Label" 
                          style={{ flex: 1 }}
                          value={opt.label}
                          onChange={(e) => {
                            const next = [...(field.options || [])];
                            next[i] = { ...opt, label: e.target.value };
                            onUpdate({ options: next });
                          }}
                        />
                        <Box pt={i === 0 ? "xl" : 0}>
                          <ActionIcon aria-label="Delete field" color="red" variant="subtle" size="sm" onClick={() => {
                              onUpdate({ options: field.options?.filter((_, idx) => idx !== i) });
                          }}>
                            <Trash2 size={12} />
                          </ActionIcon>
                        </Box>
                      </Group>
                    ))}
                    <Button size="compact-xs" variant="light" onClick={() => {
                      onUpdate({ options: [...(field.options || []), { value: '', label: '' }] });
                    }}>Add Option</Button>
                  </Stack>
                </Box>
              )}
            </Stack>
          </Tabs.Panel>

          <Tabs.Panel value="validation" pt="xs">
            <Stack gap="xs">
              <TextInput 
                label="RegEx Pattern" 
                size="xs"
                placeholder="e.g. ^[0-9]+$"
                description="Regular expression for validation"
                value={field.validation?.pattern || ''}
                onChange={(e) => onUpdate({ validation: { ...field.validation, pattern: e.target.value } })}
              />
              <TextInput 
                label="Error Message" 
                size="xs"
                placeholder="Custom error message"
                description="Text shown when validation fails"
                value={field.validation?.message || ''}
                onChange={(e) => onUpdate({ validation: { ...field.validation, message: e.target.value } })}
              />
              <TextInput 
                label="Custom JS Validation" 
                size="xs"
                placeholder="value => value > 10"
                description="JavaScript logic for complex checks"
                value={field.validation?.customJs || ''}
                onChange={(e) => onUpdate({ validation: { ...field.validation, customJs: e.target.value } })}
              />
            </Stack>
          </Tabs.Panel>

          <Tabs.Panel value="logic" pt="xs">
            <Stack gap="xs">
              <VisualConditionBuilder 
                title="HIDDEN IF"
                condition={field.logic?.hiddenIf || ''} 
                onChange={(c) => onUpdate({ logic: { ...field.logic, hiddenIf: c } })} 
              />
              <VisualConditionBuilder 
                title="DISABLED IF"
                condition={field.logic?.disabledIf || ''} 
                onChange={(c) => onUpdate({ logic: { ...field.logic, disabledIf: c } })} 
              />
            </Stack>
          </Tabs.Panel>

          <Tabs.Panel value="data" pt="xs">
            <Stack gap="xs">
              <Select 
                label="Data Source Type"
                size="xs"
                description="Where to load options from"
                data={[
                  { value: 'static', label: 'Static Options' },
                  { value: 'variable', label: 'Process Variable' },
                  { value: 'endpoint', label: 'External Endpoint' },
                ]}
                value={field.dataSource?.type || 'static'}
                onChange={(val) => onUpdate({ dataSource: { ...field.dataSource, type: val as any } })}
              />
              {field.dataSource?.type === 'variable' && (
                <TextInput 
                  label="Variable Key" 
                  size="xs"
                  placeholder="e.g. availableItems"
                  description="Process variable containing list of options"
                  value={field.dataSource?.variableKey || ''}
                  onChange={(e) => onUpdate({ dataSource: { ...field.dataSource, type: field.dataSource?.type || 'static', variableKey: e.target.value } })}
                />
              )}
              {field.dataSource?.type === 'endpoint' && (
                <TextInput 
                  label="Endpoint URL" 
                  size="xs"
                  placeholder="https://api.example.com/items"
                  description="REST API returning list of options"
                  value={field.dataSource?.endpointUrl || ''}
                  onChange={(e) => onUpdate({ dataSource: { ...field.dataSource, type: field.dataSource?.type || 'static', endpointUrl: e.target.value } })}
                />
              )}
            </Stack>
          </Tabs.Panel>
        </Tabs>
      </Stack>
    </Paper>
  );
}
