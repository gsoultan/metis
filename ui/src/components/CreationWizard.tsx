import { useState, useCallback } from 'react';
import { 
  Modal, 
  Stepper, 
  Button, 
  Group, 
  TextInput, 
  Stack, 
  Text, 
  SimpleGrid, 
  UnstyledButton, 
  Paper, 
  ThemeIcon,
  Box,
  Title,
} from '@mantine/core';
import { 
  Workflow, 
  Table2, 
  ChevronRight, 
  Info,
  Rocket,
  ArrowRight,
} from 'lucide-react';

interface CreationWizardProps {
  opened: boolean;
  onClose: () => void;
  onCreateProcess: (data: { name: string; key: string }) => void;
  onCreateDecision: (data: { name: string; key: string }) => void;
  initialType?: 'process' | 'decision';
}

export function CreationWizard({ opened, onClose, onCreateProcess, onCreateDecision, initialType }: CreationWizardProps) {
  const [active, setActive] = useState(initialType ? 1 : 0);
  const [type, setType] = useState<'process' | 'decision' | null>(initialType || null);
  const [name, setName] = useState(initialType === 'process' ? 'New Process' : initialType === 'decision' ? 'New Decision' : '');
  const [key, setKey] = useState(initialType === 'process' ? 'new_process' : initialType === 'decision' ? 'new_decision' : '');

  const reset = useCallback(() => {
    setActive(initialType ? 1 : 0);
    setType(initialType || null);
    setName(initialType === 'process' ? 'New Process' : initialType === 'decision' ? 'New Decision' : '');
    setKey(initialType === 'process' ? 'new_process' : initialType === 'decision' ? 'new_decision' : '');
  }, [initialType]);

  // Reset when the wizard transitions closed → open, during render rather than
  // in an effect. As an effect it also re-fired whenever `reset` changed
  // identity (it depends on initialType), discarding input the user had already
  // entered in an open wizard.
  const [wasOpened, setWasOpened] = useState(opened);
  if (opened !== wasOpened) {
    setWasOpened(opened);
    if (opened) reset();
  }

  const nextStep = () => setActive((current) => (current < 2 ? current + 1 : current));
  const prevStep = () => setActive((current) => (current > 0 ? current - 1 : current));

  const handleCreate = () => {
    if (type === 'process') {
      onCreateProcess({ name, key });
    } else {
      onCreateDecision({ name, key });
    }
    onClose();
    reset();
  };

  const handleTypeSelect = (selectedType: 'process' | 'decision') => {
    setType(selectedType);
    setName(selectedType === 'process' ? 'New Process' : 'New Decision');
    setKey(selectedType === 'process' ? 'new_process' : 'new_decision');
    nextStep();
  };

  const isStep1Valid = !!type;
  const isStep2Valid = !!name && !!key;

  return (
    <Modal 
      opened={opened} 
      onClose={() => { onClose(); reset(); }} 
      size="lg" 
      title={<Group gap="xs"><Rocket size={20} color="var(--mantine-color-indigo-6)" /><Title order={4}>Creation Wizard</Title></Group>}
      radius="md"
      padding="xl"
    >
      <Stepper active={active} onStepClick={setActive} allowNextStepsSelect={false} color="indigo" size="sm">
        <Stepper.Step label="Define Type" description="What are you building?" icon={<Workflow size={18} />}>
          <Box pt="xl">
            <Text size="sm" c="dimmed" mb="xl">Choose the core element you want to define. You can always connect them later.</Text>
            <SimpleGrid cols={2} spacing="md">
              <TypeButton 
                icon={<Workflow size={32} />} 
                title="BPMN Process" 
                description="Visually model a business process flow with tasks and events." 
                color="blue"
                onClick={() => handleTypeSelect('process')}
                active={type === 'process'}
              />
              <TypeButton 
                icon={<Table2 size={32} />} 
                title="Decision Table" 
                description="Define complex business rules in a spreadsheet-style format." 
                color="teal"
                onClick={() => handleTypeSelect('decision')}
                active={type === 'decision'}
              />
            </SimpleGrid>
          </Box>
        </Stepper.Step>

        <Stepper.Step label="Basic Info" description="Identity & Metadata" icon={<Info size={18} />}>
          <Stack gap="md" pt="xl">
            <TextInput 
              label="Display Name" 
              placeholder={type === 'process' ? "e.g. Employee Onboarding" : "e.g. Risk Assessment"} 
              value={name} 
              onChange={(e) => {
                setName(e.currentTarget.value);
                if (!key || key === 'new_process' || key === 'new_decision') {
                   setKey(e.currentTarget.value.toLowerCase().replace(/\s+/g, '_'));
                }
              }}
              required
              autoFocus
            />
            <TextInput 
              label="Technical Key" 
              placeholder="e.g. employee_onboarding" 
              value={key} 
              onChange={(e) => setKey(e.currentTarget.value.toLowerCase().replace(/[^a-z0-9_]/g, '_'))}
              description="Used internally and in API calls. Only lowercase letters, numbers, and underscores."
              required
              error={key.includes(' ') || /[^a-z0-9_]/.test(key) ? "Invalid key format" : null}
            />
            
            <Paper withBorder p="md" bg="gray.0" radius="md" mt="md">
               <Group wrap="nowrap" align="flex-start">
                  <ThemeIcon variant="light" color="indigo" size="sm">
                    <Info size={14} />
                  </ThemeIcon>
                  <Text size="xs" c="dimmed">
                    The <b>Technical Key</b> should be unique within your project. We recommend using snake_case.
                  </Text>
               </Group>
            </Paper>
          </Stack>
        </Stepper.Step>

        <Stepper.Completed>
          <Stack align="center" py="xl" gap="sm">
            <ThemeIcon size={60} radius="xl" variant="light" color="green">
              <Rocket size={32} />
            </ThemeIcon>
            <Title order={3}>Ready to Go!</Title>
            <Text c="dimmed" ta="center" maw={350}>
              We'll set up your {type === 'process' ? 'BPMN canvas' : 'Decision grid'} with your initial settings.
            </Text>
            
            <Paper withBorder p="lg" radius="md" mt="md" w="100%">
               <Group justify="space-between">
                  <Box>
                    <Text size="xs" fw={700} c="dimmed" tt="uppercase">Element</Text>
                    <Text fw={700}>{name}</Text>
                  </Box>
                  <Box ta="right">
                    <Text size="xs" fw={700} c="dimmed" tt="uppercase">Type</Text>
                    <Text fw={700} c="indigo">{type?.toUpperCase()}</Text>
                  </Box>
               </Group>
            </Paper>
          </Stack>
        </Stepper.Completed>
      </Stepper>

      <Group justify="flex-end" mt="xl">
        {active > 0 && (
          <Button variant="default" onClick={prevStep}>Back</Button>
        )}
        {active < 2 ? (
          <Button 
            onClick={nextStep} 
            disabled={active === 0 ? !isStep1Valid : !isStep2Valid}
            rightSection={<ChevronRight size={16} />}
            color="indigo"
          >
            Next Step
          </Button>
        ) : (
          <Button color="indigo" leftSection={<ArrowRight size={16} />} onClick={handleCreate}>
            Create Element
          </Button>
        )}
      </Group>
    </Modal>
  );
}

function TypeButton({ icon, title, description, color, onClick, active }: any) {
  return (
    <UnstyledButton 
      onClick={onClick}
      style={(theme) => ({
        padding: theme.spacing.xl,
        borderRadius: theme.radius.lg,
        border: `2px solid ${active ? `var(--mantine-color-${color}-6)` : 'var(--mantine-color-default-border)'}`,
        backgroundColor: active ? `var(--mantine-color-${color}-0)` : 'white',
        transition: 'all 0.2s ease',
        '&:hover': {
          backgroundColor: 'var(--mantine-color-gray-0)',
          borderColor: active ? `var(--mantine-color-${color}-6)` : 'var(--mantine-color-gray-3)',
          transform: 'translateY(-2px)',
        }
      })}
    >
      <Stack align="center" gap="sm">
        <ThemeIcon size={64} radius="xl" variant="light" color={color}>
          {icon}
        </ThemeIcon>
        <Text fw={800} size="lg">{title}</Text>
        <Text size="xs" c="dimmed" ta="center" lh={1.4}>{description}</Text>
      </Stack>
    </UnstyledButton>
  );
}
