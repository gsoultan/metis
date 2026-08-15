import { 
  Stack, 
  TextInput, 
  NumberInput, 
  Select, 
  Checkbox, 
  Textarea, 
  Button, 
  Group, 
  Text,
  Divider,
  Grid,
  Tooltip,
  ActionIcon
} from '@mantine/core';
import { type FormField } from './FormBuilder';
import { DatePickerInput } from '@mantine/dates';
import { CheckCircle, Info } from 'lucide-react';
import { useTaskForm } from '../hooks/useTaskForm';
import { asText, asNumber } from '../types/bpmn';

interface TaskFormProps {
  fields: FormField[];
  variables: Record<string, unknown>;
  onSubmit: (values: Record<string, unknown>) => void;
  loading?: boolean;
}

export function TaskForm({ fields, variables, onSubmit, loading }: TaskFormProps) {
  const {
    values,
    errors,
    visibleFields,
    handleSubmit,
    handleChange,
    handleBlur,
    isDisabled,
    getSelectOptions,
  } = useTaskForm(fields, variables, onSubmit);

  const renderFieldControl = (field: FormField, disabled: boolean) => {
    const rendererRegistry = {
      text: () => (
        <TextInput
          label={field.label}
          description={field.description}
          placeholder={field.placeholder}
          required={field.required}
          disabled={disabled}
          error={errors[field.id]}
          value={asText(values[field.id])}
          onChange={(event) => handleChange(field.id, event.target.value)}
          onBlur={() => handleBlur(field.id)}
        />
      ),
      textarea: () => (
        <Textarea
          label={field.label}
          description={field.description}
          placeholder={field.placeholder}
          required={field.required}
          disabled={disabled}
          error={errors[field.id]}
          minRows={3}
          value={asText(values[field.id])}
          onChange={(event) => handleChange(field.id, event.target.value)}
          onBlur={() => handleBlur(field.id)}
        />
      ),
      number: () => (
        <NumberInput
          label={field.label}
          description={field.description}
          placeholder={field.placeholder}
          required={field.required}
          disabled={disabled}
          error={errors[field.id]}
          value={asNumber(values[field.id])}
          onChange={(value) => handleChange(field.id, value)}
          onBlur={() => handleBlur(field.id)}
        />
      ),
      date: () => (
        <DatePickerInput
          label={field.label}
          description={field.description}
          placeholder={field.placeholder}
          required={field.required}
          disabled={disabled}
          error={errors[field.id]}
          value={values[field.id] ? new Date(asText(values[field.id])) : null}
          onChange={(value) => handleChange(field.id, value)}
          onBlur={() => handleBlur(field.id)}
        />
      ),
      select: () => (
        <Select
          label={field.label}
          description={field.description}
          placeholder={field.placeholder}
          required={field.required}
          disabled={disabled}
          error={errors[field.id]}
          data={getSelectOptions(field)}
          value={asText(values[field.id]) || null}
          onChange={(value) => handleChange(field.id, value)}
          onBlur={() => handleBlur(field.id)}
        />
      ),
      boolean: () => (
        <Checkbox
          label={field.label}
          description={field.description}
          disabled={disabled}
          checked={!!values[field.id]}
          onChange={(event) => handleChange(field.id, event.target.checked)}
          onBlur={() => handleBlur(field.id)}
          mt="md"
        />
      ),
    };

    const renderer = rendererRegistry[field.type as keyof typeof rendererRegistry];
    return renderer ? renderer() : null;
  };

  if (fields.length === 0) {
    return (
        <Stack align="center" py="xl">
            <Text size="sm" c="dimmed">No inputs required for this task.</Text>
            <Button 
                onClick={() => onSubmit({})} 
                loading={loading}
                leftSection={<CheckCircle size={16} />}
            >
                Complete Task
            </Button>
        </Stack>
    );
  }

  return (
    <form onSubmit={handleSubmit}>
      <Grid gap="md">
        {visibleFields.map((field) => {
          const disabled = isDisabled(field);
          const span = field.gridSpan || 1;

          if (field.type === 'section') {
            return (
              <Grid.Col key={field.id} span={12}>
                <Divider 
                  my="xs" 
                  label={
                    <Group gap={4}>
                      <Text fw={700} size="sm">{field.label}</Text>
                      {field.description && (
                        <Tooltip label={field.description}>
                          <ActionIcon aria-label="Details" variant="transparent" color="gray" size="xs">
                            <Info size={14} />
                          </ActionIcon>
                        </Tooltip>
                      )}
                    </Group>
                  } 
                  labelPosition="left" 
                />
              </Grid.Col>
            );
          }

          return (
            <Grid.Col key={field.id} span={span === 1 ? 6 : 12}>
              {renderFieldControl(field, disabled)}
            </Grid.Col>
          );
        })}
      </Grid>
      
      <Divider mt="xl" mb="md" />
      
      <Group justify="flex-end">
        <Button 
          type="submit" 
          loading={loading}
          leftSection={<CheckCircle size={16} />}
        >
          Complete Task
        </Button>
      </Group>
    </form>
  );
}
