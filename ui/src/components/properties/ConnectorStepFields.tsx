import { Button, Group, Text, Textarea, TextInput } from '@mantine/core';
import { Plus } from 'lucide-react';

import { queryParameters, stepFieldPatch, stepFieldValue } from '../../domain/connectorStep';
import type { ApiConnectorProperty } from '../../services/types';
import { asText, asTextMap } from '../../types/bpmn';
import { MappingTable } from './CommonProperties';
import { PropertySection } from './PropertySection';

interface ConnectorStepFieldsProps {
  schema: readonly ApiConnectorProperty[];
  data: Record<string, unknown>;
  onUpdate: (patch: Record<string, unknown>) => void;
}

/**
 * What a step fills in for a connector that asks for it, drawn from the
 * connector's own description of its fields — so another connector of this
 * kind needs nothing here.
 *
 * The connection itself — the server, the login — is not here. That is set up
 * once on the Connectors page, by whoever may see the password.
 */
export function ConnectorStepFields({ schema, data, onUpdate }: ConnectorStepFieldsProps) {
  return (
    <PropertySection
      title="This step"
      hint="The connection itself is set up once, on the Connectors page. What goes here is what this step asks it."
    >
      {schema.map((field) => (
        <StepField key={field.key} field={field} data={data} onUpdate={onUpdate} />
      ))}
    </PropertySection>
  );
}

interface StepFieldProps {
  field: ApiConnectorProperty;
  data: Record<string, unknown>;
  onUpdate: (patch: Record<string, unknown>) => void;
}

function StepField({ field, data, onUpdate }: StepFieldProps) {
  const value = stepFieldValue(data, field.key);
  const set = (next: unknown) => onUpdate(stepFieldPatch(field.key, next));
  const common = { label: field.label, description: field.description, required: field.required };

  switch (field.type) {
    case 'textarea':
      return (
        <Textarea
          {...common}
          value={asText(value)}
          onChange={(e) => set(e.currentTarget.value)}
          autosize
          minRows={4}
          styles={{ input: { fontFamily: 'var(--mantine-font-family-monospace)', fontSize: 12 } }}
        />
      );
    case 'mapping':
      return <ValuesField field={field} statement={asText(stepFieldValue(data, 'connector_statement'))} value={asTextMap(value)} onChange={set} />;
    default:
      return <TextInput {...common} value={asText(value)} onChange={(e) => set(e.currentTarget.value)} />;
  }
}

interface ValuesFieldProps {
  field: ApiConnectorProperty;
  statement: string;
  value: Record<string, string>;
  onChange: (value: Record<string, string>) => void;
}

/**
 * Where each :name in the query takes its value from. A name the query uses
 * and the step has not given a value is offered as a one-click row, since
 * typing it again is how it gets spelled differently.
 */
function ValuesField({ field, statement, value, onChange }: ValuesFieldProps) {
  const waiting = queryParameters(statement).filter((name) => !(name in value));
  return (
    <>
      <MappingTable
        title={field.label}
        sourceLabel="In the query"
        targetLabel="From the process"
        mapping={value}
        onUpdate={onChange}
      />
      {field.description && <Text size="xs" c="dimmed">{field.description}</Text>}
      {waiting.length > 0 && (
        <Group gap={6}>
          {waiting.map((name) => (
            <Button
              key={name}
              size="compact-xs"
              variant="light"
              leftSection={<Plus size={12} />}
              onClick={() => onChange({ ...value, [name]: '' })}
            >
              :{name}
            </Button>
          ))}
        </Group>
      )}
    </>
  );
}
