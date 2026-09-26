import {
  Stepper,
  Button,
  Group,
  TextInput,
  PasswordInput,
  Paper,
  Title,
  Text,
  Container,
  Stack,
  ThemeIcon,
  Box,
  Alert,
  Center,
  Select,
  NumberInput,
  Switch,
  ActionIcon,
  Tooltip,
  CopyButton,
  Progress,
  useMantineTheme,
} from '@mantine/core';
import { useForm } from '@mantine/form';
import { useQuery } from '@tanstack/react-query';
import { useState, useCallback } from 'react';
import {
  ShieldCheck,
  Building2,
  LayoutDashboard,
  CheckCircle2,
  AlertCircle,
  Workflow,
  Database,
  KeyRound,
  RefreshCw,
  Copy,
  Check,
  PlugZap,
  Loader2,
} from 'lucide-react';
import { processService } from '../services/api';
import { useAppStore } from '../store/useAppStore';
import { MIN_PASSWORD_LENGTH } from '../domain/password';
import { firstSetupStep, SETUP_STEPS, setupRequestFor } from '../domain/setupWizard';
import { setupService } from '../services/domains/setupService';
import { errorMessage } from '../services/shared/errors';
import { SetupComplete } from '../components/setup/SetupComplete';
import { useEffect } from 'react';

// PostgreSQL is the only engine this runs on. The list stays a list rather than
// becoming a fixed label because the shape of the step — pick an engine, give
// its connection — is what a second one would need, and a select with one
// option says "this is the choice" more honestly than a hidden field.
const DATABASE_DRIVERS = [
  { value: 'postgres', label: 'PostgreSQL' },
] as const;

const POSTGRES_PORT = 5432;

const MIN_ENCRYPTION_KEY_LENGTH = 16;
const GENERATED_KEY_LENGTH = 32;
const GENERATED_PASSWORD_LENGTH = 16;

const CRYPTO_CHARSET = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789!@#$%^&*()-_=+';
const ALPHANUM_CHARSET = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';

function generateSecureRandom(length: number, charset: string): string {
  const array = new Uint32Array(length);
  crypto.getRandomValues(array);
  return Array.from(array, (v) => charset[v % charset.length]).join('');
}

function getPasswordStrength(password: string): number {
  if (password.length === 0) return 0;
  let score = 0;
  if (password.length >= 6) score += 20;
  if (password.length >= 10) score += 20;
  if (/[a-z]/.test(password) && /[A-Z]/.test(password)) score += 20;
  if (/\d/.test(password)) score += 20;
  if (/[^a-zA-Z0-9]/.test(password)) score += 20;
  return score;
}

function getStrengthColor(strength: number): string {
  if (strength < 40) return 'red';
  if (strength < 70) return 'yellow';
  return 'green';
}

function getStrengthLabel(strength: number): string {
  if (strength < 40) return 'Weak';
  if (strength < 70) return 'Moderate';
  return 'Strong';
}

export function Setup({ onComplete }: { onComplete: () => void }) {
  const [active, setActive] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [testingConnection, setTestingConnection] = useState(false);
  const [connectionResult, setConnectionResult] = useState<{ success: boolean; message: string } | null>(null);
  const theme = useMantineTheme();
  const { clearAuth } = useAppStore();

  useEffect(() => {
    // Clear any existing auth state when entering setup
    clearAuth();
  }, [clearAuth]);

  // The route guard has already read this; the wizard reads it again for the
  // one thing it decides here — whether the environment has named the database
  // and secrets, in which case there is nothing to ask about either.
  const { data: setupState } = useQuery({
    queryKey: ['setup-status'],
    queryFn: ({ signal }) => setupService.getSetupStatus(signal),
    staleTime: Infinity,
  });
  const fromEnvironment = setupState?.status?.configured_by_environment === true;
  const firstStep = firstSetupStep(fromEnvironment);
  // Derived rather than set: the answer arrives after the first render, and a
  // wizard that had already started on the database step moves past it.
  const step = Math.max(active, firstStep);

  const form = useForm({
    initialValues: {
      database_driver: 'postgres',
      db_host: 'localhost',
      db_port: 5432,
      db_username: '',
      db_password: '',
      db_name: '',
      db_ssl_enabled: false,
      encryption_key: '',
      jwt_secret: '',
      admin_username: 'admin',
      admin_password: '',
      admin_full_name: '',
      admin_public_name: '',
      admin_email: '',
      organization_name: '',
      project_name: 'Default Project',
    },
    validate: (values) => {
      if (step === SETUP_STEPS.database) {
        if (!values.database_driver) {
          return { database_driver: 'Database driver is required' };
        }
        const errors: Record<string, string | null> = {};
        if (!values.db_host) errors.db_host = 'Host is required';
        if (!values.db_port) errors.db_port = 'Port is required';
        if (!values.db_username) errors.db_username = 'Username is required';
        if (!values.db_name) errors.db_name = 'Database name is required';
        if (Object.keys(errors).length > 0) return errors;
        return {};
      }
      if (step === SETUP_STEPS.security) {
        return {
          encryption_key:
            !values.encryption_key
              ? 'Encryption key is required'
              : values.encryption_key.length < MIN_ENCRYPTION_KEY_LENGTH
                ? `Encryption key must be at least ${MIN_ENCRYPTION_KEY_LENGTH} characters`
                : null,
          jwt_secret:
            !values.jwt_secret
              ? 'JWT secret is required'
              : values.jwt_secret.length < MIN_ENCRYPTION_KEY_LENGTH
                ? `JWT secret must be at least ${MIN_ENCRYPTION_KEY_LENGTH} characters`
                : null,
        };
      }
      if (step === SETUP_STEPS.administrator) {
        return {
          admin_username: values.admin_username.length < 3 ? 'Username must be at least 3 characters' : null,
          admin_password: values.admin_password.length < MIN_PASSWORD_LENGTH ? `Password must be at least ${MIN_PASSWORD_LENGTH} characters` : null,
          admin_full_name: !values.admin_full_name ? 'Full name is required' : null,
          admin_public_name: !values.admin_public_name ? 'Public name is required' : null,
          admin_email: !/^\S+@\S+$/.test(values.admin_email) ? 'Invalid email' : null,
        };
      }
      if (step === SETUP_STEPS.organization) {
        return {
          organization_name: values.organization_name.length < 2 ? 'Organization name is required' : null,
        };
      }
      return {};
    },
  });

  const handleDriverChange = useCallback((value: string | null) => {
    if (!value) return;
    form.setFieldValue('database_driver', value);
    form.setFieldValue('db_port', POSTGRES_PORT);
  }, [form]);

  const generateEncryptionKey = useCallback(() => {
    const key = generateSecureRandom(GENERATED_KEY_LENGTH, ALPHANUM_CHARSET);
    form.setFieldValue('encryption_key', key);
  }, [form]);

  const generateJWTSecret = useCallback(() => {
    const secret = generateSecureRandom(GENERATED_KEY_LENGTH, CRYPTO_CHARSET);
    form.setFieldValue('jwt_secret', secret);
  }, [form]);

  const generatePassword = useCallback(() => {
    const password = generateSecureRandom(GENERATED_PASSWORD_LENGTH, CRYPTO_CHARSET);
    form.setFieldValue('admin_password', password);
  }, [form]);

  const handleTestConnection = useCallback(async () => {
    setTestingConnection(true);
    setConnectionResult(null);
    try {
      const result = await processService.testConnection({
        database_driver: form.values.database_driver,
        db_host: form.values.db_host,
        db_port: form.values.db_port,
        db_username: form.values.db_username,
        db_password: form.values.db_password,
        db_name: form.values.db_name,
        db_ssl_enabled: form.values.db_ssl_enabled,
      });
      setConnectionResult(result);
    } catch (err: unknown) {
      const message = errorMessage(err, 'Connection test failed');
      setConnectionResult({ success: false, message });
    } finally {
      setTestingConnection(false);
    }
  }, [form.values]);

  const nextStep = () => {
    const validation = form.validate();
    if (!validation.hasErrors) {
      setActive(Math.min(step + 1, SETUP_STEPS.done));
    }
  };

  const prevStep = () => setActive(Math.max(step - 1, firstStep));

  const handleSetup = async () => {
    setLoading(true);
    setError(null);
    try {
      const { err } = await processService.setup(setupRequestFor(form.values, fromEnvironment));
      if (err) {
        setError(errorMessage(err, 'Setup failed'));
      } else {
        setActive(SETUP_STEPS.done);
      }
    } catch (err: unknown) {
      const message = errorMessage(err, 'Setup failed');
      setError(message);
    } finally {
      setLoading(false);
    }
  };
  const passwordStrength = getPasswordStrength(form.values.admin_password);

  return (
    <Box
      mih="100vh"
      py="xl"
      style={{
        background: `linear-gradient(135deg, ${theme.colors.gray[0]} 0%, ${theme.colors.gray[2]} 100%)`,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        overflowY: 'auto',
      }}
    >
      {/* lg, not md: five steps with a label and a description need about
          1100px, and md left them ~900 — so they wrapped, stranding the
          connector after the fourth pointing at nothing. */}
      <Container size="lg" w="100%">
        <Paper shadow="xl" radius="lg" p={40} withBorder>
          <Stack gap="xl">
            <Center>
              <Group gap="sm">
                <ThemeIcon size={48} radius="md" variant="gradient" gradient={{ from: 'blue', to: 'cyan' }}>
                  <Workflow size={28} />
                </ThemeIcon>
                <div>
                  <Title order={2} fw={900} lts={-0.5}>Metis</Title>
                  <Text size="xs" c="dimmed" fw={700} tt="uppercase">System Setup Wizard</Text>
                </div>
              </Group>
            </Center>

            <Stepper
              active={step}
              onStepClick={(clicked) => setActive(Math.max(clicked, firstStep))}
              allowNextStepsSelect={false}
              size="sm"
            >
              {/* Step 0: Database Configuration */}
              <Stepper.Step
                label="Database"
                description={fromEnvironment ? 'Set by the environment' : 'Connection settings'}
                icon={<Database size={18} />}
              >
                <Stack gap="md" mt="xl">
                  <Title order={4}>Database Configuration</Title>
                  <Text size="sm" c="dimmed">
                    Choose your database engine and provide the connection details.
                    The connection credentials will be encrypted before being stored.
                  </Text>
                  <Select
                    label="Database Driver"
                    placeholder="Select a database driver"
                    data={[...DATABASE_DRIVERS]}
                    required
                    value={form.values.database_driver}
                    onChange={handleDriverChange}
                    error={form.errors.database_driver}
                  />
                  <Alert variant="light" color="blue" icon={<Database size={16} />}>
                    Metis runs on PostgreSQL. Point it at an empty database — it creates its own
                    tables on first start, and will not touch anything already there.
                  </Alert>
                  {(
                    <>
                      <Group grow>
                        <TextInput
                          label="Host"
                          placeholder="localhost"
                          required
                          {...form.getInputProps('db_host')}
                        />
                        <NumberInput
                          label="Port"
                          placeholder={String(POSTGRES_PORT)}
                          required
                          min={1}
                          max={65535}
                          {...form.getInputProps('db_port')}
                        />
                      </Group>
                      <Group grow>
                        <TextInput
                          label="Username"
                          placeholder="metis"
                          required
                          {...form.getInputProps('db_username')}
                        />
                        <PasswordInput
                          label="Password"
                          placeholder="Database password"
                          {...form.getInputProps('db_password')}
                        />
                      </Group>
                      <TextInput
                        label="Database Name"
                        placeholder="metis"
                        required
                        {...form.getInputProps('db_name')}
                      />
                      <Switch
                        label="Enable SSL / TLS"
                        description="Enable encrypted connection to the database server"
                        {...form.getInputProps('db_ssl_enabled', { type: 'checkbox' })}
                      />
                    </>
                  )}
                  <Button
                    variant="light"
                    leftSection={testingConnection ? <Loader2 size={16} className="animate-spin" /> : <PlugZap size={16} />}
                    loading={testingConnection}
                    onClick={handleTestConnection}
                  >
                    Test Connection
                  </Button>
                  {connectionResult && (
                    <Alert
                      variant="light"
                      color={connectionResult.success ? 'green' : 'red'}
                      icon={connectionResult.success ? <CheckCircle2 size={16} /> : <AlertCircle size={16} />}
                      title={connectionResult.success ? 'Connection Successful' : 'Connection Failed'}
                    >
                      {connectionResult.message}
                    </Alert>
                  )}
                </Stack>
              </Stepper.Step>

              {/* Step 1: Encryption Key */}
              <Stepper.Step
                label="Security"
                description={fromEnvironment ? 'Set by the environment' : 'Encryption key'}
                icon={<KeyRound size={18} />}
              >
                <Stack gap="md" mt="xl">
                  <Title order={4}>Security Settings</Title>
                  <Text size="sm" c="dimmed">
                    These keys are used to secure your installation.
                    The encryption key protects sensitive data in the database, while the JWT secret signs authentication tokens.
                    Store them securely — they cannot be recovered if lost.
                  </Text>
                  <div>
                    <Group align="flex-end" gap="xs">
                      <PasswordInput
                        label="Encryption Key"
                        placeholder="Enter or generate a strong encryption key"
                        description={`Used for AES-256-GCM. Must be at least ${MIN_ENCRYPTION_KEY_LENGTH} characters.`}
                        required
                        style={{ flex: 1 }}
                        {...form.getInputProps('encryption_key')}
                      />
                      <Tooltip label="Generate secure key">
                        <ActionIcon aria-label="Generate encryption key"
                          variant="light"
                          color="blue"
                          size="lg"
                          mb={form.errors.encryption_key ? 22 : 0}
                          onClick={generateEncryptionKey}
                        >
                          <RefreshCw size={18} />
                        </ActionIcon>
                      </Tooltip>
                      <CopyButton value={form.values.encryption_key} timeout={2000}>
                        {({ copied, copy }) => (
                          <Tooltip label={copied ? 'Copied' : 'Copy to clipboard'}>
                            <ActionIcon aria-label="Copy encryption key"
                              variant="light"
                              color={copied ? 'green' : 'gray'}
                              size="lg"
                              mb={form.errors.encryption_key ? 22 : 0}
                              onClick={copy}
                            >
                              {copied ? <Check size={18} /> : <Copy size={18} />}
                            </ActionIcon>
                          </Tooltip>
                        )}
                      </CopyButton>
                    </Group>
                  </div>
                  <div>
                    <Group align="flex-end" gap="xs">
                      <PasswordInput
                        label="JWT Secret"
                        placeholder="Enter or generate a strong JWT secret"
                        description={`Used for signing tokens. Must be at least ${MIN_ENCRYPTION_KEY_LENGTH} characters.`}
                        required
                        style={{ flex: 1 }}
                        {...form.getInputProps('jwt_secret')}
                      />
                      <Tooltip label="Generate secure secret">
                        <ActionIcon aria-label="Generate JWT secret"
                          variant="light"
                          color="blue"
                          size="lg"
                          mb={form.errors.jwt_secret ? 22 : 0}
                          onClick={generateJWTSecret}
                        >
                          <RefreshCw size={18} />
                        </ActionIcon>
                      </Tooltip>
                      <CopyButton value={form.values.jwt_secret} timeout={2000}>
                        {({ copied, copy }) => (
                          <Tooltip label={copied ? 'Copied' : 'Copy to clipboard'}>
                            <ActionIcon aria-label="Copy JWT secret"
                              variant="light"
                              color={copied ? 'green' : 'gray'}
                              size="lg"
                              mb={form.errors.jwt_secret ? 22 : 0}
                              onClick={copy}
                            >
                              {copied ? <Check size={18} /> : <Copy size={18} />}
                            </ActionIcon>
                          </Tooltip>
                        )}
                      </CopyButton>
                    </Group>
                  </div>
                  <Alert variant="light" color="orange" icon={<KeyRound size={16} />}>
                    <strong>Important:</strong> This key cannot be recovered if lost. Write it down and store it
                    in a secure location. Without it, encrypted configuration data cannot be decrypted.
                  </Alert>
                </Stack>
              </Stepper.Step>

              {/* Step 2: Administrator */}
              <Stepper.Step
                label="Administrator"
                description="Create root account"
                icon={<ShieldCheck size={18} />}
              >
                <Stack gap="md" mt="xl">
                  <Title order={4}>Administrator Settings</Title>
                  <Text size="sm" c="dimmed">This account will have full access to the system.</Text>
                  <TextInput
                    label="Admin Username"
                    placeholder="admin"
                    required
                    {...form.getInputProps('admin_username')}
                  />
                  <Group grow>
                    <TextInput
                      label="Full Name"
                      placeholder="John Doe"
                      required
                      {...form.getInputProps('admin_full_name')}
                    />
                    <TextInput
                      label="Public Name"
                      placeholder="jdoe"
                      required
                      {...form.getInputProps('admin_public_name')}
                    />
                  </Group>
                  <TextInput
                    label="Admin Email"
                    placeholder="admin@example.com"
                    required
                    {...form.getInputProps('admin_email')}
                  />
                  <div>
                    <Group align="flex-end" gap="xs">
                      <PasswordInput
                        label="Admin Password"
                        placeholder="Choose a strong password"
                        required
                        style={{ flex: 1 }}
                        {...form.getInputProps('admin_password')}
                      />
                      <Tooltip label="Generate secure password">
                        <ActionIcon aria-label="Generate administrator password"
                          variant="light"
                          color="blue"
                          size="lg"
                          mb={form.errors.admin_password ? 22 : 0}
                          onClick={generatePassword}
                        >
                          <RefreshCw size={18} />
                        </ActionIcon>
                      </Tooltip>
                      <CopyButton value={form.values.admin_password} timeout={2000}>
                        {({ copied, copy }) => (
                          <Tooltip label={copied ? 'Copied' : 'Copy to clipboard'}>
                            <ActionIcon aria-label="Copy administrator password"
                              variant="light"
                              color={copied ? 'green' : 'gray'}
                              size="lg"
                              mb={form.errors.admin_password ? 22 : 0}
                              onClick={copy}
                            >
                              {copied ? <Check size={18} /> : <Copy size={18} />}
                            </ActionIcon>
                          </Tooltip>
                        )}
                      </CopyButton>
                    </Group>
                    {form.values.admin_password.length > 0 && (
                      <Stack gap={4} mt="xs">
                        <Progress
                          value={passwordStrength}
                          color={getStrengthColor(passwordStrength)}
                          size="sm"
                          radius="xl"
                        />
                        <Text size="xs" c={getStrengthColor(passwordStrength)}>
                          Password strength: {getStrengthLabel(passwordStrength)}
                        </Text>
                      </Stack>
                    )}
                  </div>
                </Stack>
              </Stepper.Step>

              {/* Step 3: Organization */}
              <Stepper.Step
                label="Organization"
                description="Corporate identity"
                icon={<Building2 size={18} />}
              >
                <Stack gap="md" mt="xl">
                  <Title order={4}>Organization Details</Title>
                  <Text size="sm" c="dimmed">Define your primary organization name.</Text>
                  <TextInput
                    label="Organization Name"
                    placeholder="e.g. Acme Corp"
                    required
                    {...form.getInputProps('organization_name')}
                  />
                </Stack>
              </Stepper.Step>

              {/* Step 4: Project */}
              <Stepper.Step
                label="Project"
                description="Initial workspace"
                icon={<LayoutDashboard size={18} />}
              >
                <Stack gap="md" mt="xl">
                  <Title order={4}>Initial Project</Title>
                  <Text size="sm" c="dimmed">Your first project to organize processes.</Text>
                  <TextInput
                    label="Project Name"
                    placeholder="e.g. Finance Automation"
                    required
                    {...form.getInputProps('project_name')}
                  />
                </Stack>
              </Stepper.Step>

              {/* Completed */}
              <Stepper.Completed>
                <SetupComplete fromEnvironment={fromEnvironment} onComplete={onComplete} />
              </Stepper.Completed>
            </Stepper>

            {error && (
              <Alert icon={<AlertCircle size={16} />} title="Setup Error" color="red" variant="light">
                {error}
              </Alert>
            )}

            {step < SETUP_STEPS.done && (
              <Group justify="flex-end" mt="xl">
                {step !== firstStep && (
                  <Button variant="default" onClick={prevStep}>
                    Back
                  </Button>
                )}
                {step < SETUP_STEPS.project ? (
                  <Button onClick={nextStep}>Next Step</Button>
                ) : (
                  <Button onClick={handleSetup} loading={loading} color="blue">
                    Finish Setup
                  </Button>
                )}
              </Group>
            )}
          </Stack>
        </Paper>
      </Container>
    </Box>
  );
}
