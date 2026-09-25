/**
 * The three-step setup for one connection: what it is, its settings, done.
 *
 * Secrets never appear here. The server sends a sentinel in place of a stored
 * credential and keeps what it has when the sentinel comes back, so a field
 * holding one shows an empty box with "unchanged" as its placeholder, and
 * clearing a replacement puts the sentinel back rather than sending nothing.
 */
import {
  Alert,
  Badge,
  Box,
  Button,
  Divider,
  Group,
  Modal,
  NumberInput,
  PasswordInput,
  Paper,
  Select,
  Stack,
  Stepper,
  Switch,
  Text,
  Textarea,
  TextInput,
  ThemeIcon,
} from '@mantine/core';
import { CheckCircle2, ChevronRight, Info, Play } from 'lucide-react';

import {
  displayValue,
  isSensitiveKey,
  isUnchanged,
  UNCHANGED_PLACEHOLDER,
  UNCHANGED_SECRET,
} from '../../domain/connectorSecrets';
import type { ApiConnector, ApiConnectorProperty } from '../../services/types';
import { ConnectorGlyph } from './ConnectorGlyph';
import type { InstanceFormState } from './instanceForm';

/** The wizard's steps, by position. */
const STEP = { introduction: 0, configuration: 1, verification: 2 } as const;

interface ConnectorSetupWizardProps {
  opened: boolean;
  onClose: () => void;
  connector: ApiConnector | null;
  /** True when changing an existing connection rather than adding one. */
  editing: boolean;
  /** The connection's config as the server sent it, so a sentinel can be put back. */
  storedConfig?: Record<string, unknown>;
  form: InstanceFormState;
  onFormChange: (form: InstanceFormState) => void;
  activeStep: number;
  onStepChange: (step: number) => void;
  onTest: () => void;
  onSave: () => void;
  saving: boolean;
}

export function ConnectorSetupWizard({
  opened,
  onClose,
  connector,
  editing,
  storedConfig,
  form,
  onFormChange,
  activeStep,
  onStepChange,
  onTest,
  onSave,
  saving,
}: ConnectorSetupWizardProps) {

  const setConfig = (key: string, value: unknown) =>
    onFormChange({ ...form, config: { ...form.config, [key]: value } });

  return (
    <Modal
      opened={opened}
      onClose={onClose}
      title={
        <Group gap="sm">
          <ThemeIcon size="md" variant="light">
            <ConnectorGlyph name={connector?.icon} size={18} />
          </ThemeIcon>
          <Text fw={800} size="lg">
            {editing ? `Edit ${connector?.name}` : `Set up ${connector?.name}`}
          </Text>
        </Group>
      }
      size="lg"
      radius="lg"
    >
      <Stack gap="xl" py="md">
        <Stepper active={activeStep} onStepClick={onStepChange} size="sm" allowNextStepsSelect={false}>
          <Stepper.Step label="Introduction" description="What is this?">
            <Stack gap="md" mt="xl">
              <Text size="sm">
                The <b>{connector?.name}</b> connector lets you{' '}
                {connector?.description?.toLowerCase() || 'integrate with an external service'}.
              </Text>
              <Alert icon={<Info size={16} />} color="blue">
                The next step asks for the credentials the service gave you. Once saved they are not shown again.
              </Alert>
              <Group justify="flex-end" mt="xl">
                <Button variant="default" onClick={onClose}>Cancel</Button>
                <Button onClick={() => onStepChange(STEP.configuration)} rightSection={<ChevronRight size={16} />}>
                  Start configuration
                </Button>
              </Group>
            </Stack>
          </Stepper.Step>

          <Stepper.Step label="Configuration" description="Credentials and settings">
            <Stack gap="md" mt="xl">
              <TextInput
                label="Friendly name"
                description="How this connection is referred to in your process models"
                placeholder="e.g., Marketing Slack Bot"
                required
                value={form.name}
                onChange={(e) => onFormChange({ ...form, name: e.currentTarget.value })}
              />
              <Divider label="Provider settings" labelPosition="center" />
              {(connector?.schema ?? []).map((prop) => (
                <ConfigField
                  key={prop.key}
                  prop={prop}
                  value={form.config[prop.key]}
                  keepsStored={isUnchanged(storedConfig?.[prop.key])}
                  onChange={(value) => setConfig(prop.key, value)}
                />
              ))}
              <Group justify="space-between" mt="xl">
                <Button variant="default" onClick={() => onStepChange(STEP.introduction)}>Back</Button>
                <Group>
                  <Button variant="light" color="orange" leftSection={<Play size={16} />} onClick={onTest}>
                    Test connection
                  </Button>
                  <Button onClick={() => onStepChange(STEP.verification)}>Next</Button>
                </Group>
              </Group>
            </Stack>
          </Stepper.Step>

          <Stepper.Step label="Verification" description="Ready to use">
            <Stack gap="md" mt="xl" align="center" ta="center">
              <ThemeIcon size={60} radius="xl" color="green" variant="light">
                <CheckCircle2 size={32} />
              </ThemeIcon>
              <Box>
                <Text fw={700} size="lg">Ready to save</Text>
                <Text size="sm" c="dimmed">
                  Once saved, any service task in this project can use this connection.
                </Text>
              </Box>
              <Paper withBorder p="md" radius="md" bg="gray.0" w="100%" ta="left">
                <Text size="xs" fw={700} c="dimmed" mb={4}>CONNECTION</Text>
                <Group justify="space-between">
                  <Text size="sm" fw={600}>{form.name}</Text>
                  <Badge size="xs">{connector?.name}</Badge>
                </Group>
              </Paper>
              <Group justify="space-between" w="100%" mt="xl">
                <Button variant="default" onClick={() => onStepChange(STEP.configuration)}>Back</Button>
                <Button onClick={onSave} loading={saving} color="indigo" disabled={!form.name.trim()}>
                  {editing ? 'Save changes' : 'Save connection'}
                </Button>
              </Group>
            </Stack>
          </Stepper.Step>
        </Stepper>
      </Stack>
    </Modal>
  );
}

interface ConfigFieldProps {
  prop: ApiConnectorProperty;
  value: unknown;
  /** Whether the server holds a value for this key that the form never saw. */
  keepsStored: boolean;
  onChange: (value: unknown) => void;
}

/** One input of the connector's schema, rendered by its declared type. */
function ConfigField({ prop, value, keepsStored, onChange }: ConfigFieldProps) {
  const shown = displayValue(value);
  const placeholder = keepsStored && isUnchanged(value) ? UNCHANGED_PLACEHOLDER : undefined;
  // Emptying a replacement means "keep what is stored", not "store nothing".
  const onText = (text: string) => onChange(text === '' && keepsStored ? UNCHANGED_SECRET : text);
  const common = {
    label: prop.label,
    description: keepsStored ? `${prop.description ?? ''} Leave empty to keep the stored value.`.trim() : prop.description,
    required: prop.required && !keepsStored,
    placeholder,
  };

  if (prop.type === 'password' || isSensitiveKey(prop.key)) {
    return <PasswordInput {...common} value={shown} onChange={(e) => onText(e.currentTarget.value)} />;
  }
  switch (prop.type) {
    case 'number':
      return (
        <NumberInput
          {...common}
          value={shown ? Number.parseInt(shown, 10) : undefined}
          onChange={(val) => onChange(val?.toString())}
        />
      );
    case 'select':
      return (
        <Select {...common} data={(prop.options ?? []).map((o) => String(o))} value={shown || null} onChange={onChange} />
      );
    case 'textarea':
      return <Textarea {...common} value={shown} onChange={(e) => onText(e.currentTarget.value)} autosize minRows={3} />;
    case 'boolean':
      // Stored as text, like every other setting here. It used to fall through
      // to a text box, so the only way to turn one on was to type "true".
      return (
        <Switch
          label={prop.label}
          description={common.description}
          checked={shown === 'true'}
          onChange={(e) => onChange(e.currentTarget.checked ? 'true' : 'false')}
        />
      );
    default:
      return <TextInput {...common} value={shown} onChange={(e) => onText(e.currentTarget.value)} />;
  }
}
