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
import { useState, useCallback } from 'react';
import {
  ShieldCheck,
  Building2,
  LayoutDashboard,
  CheckCircle2,
  AlertCircle,
  Workflow,
  Rocket,
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
import { useEffect } from 'react';

const DATABASE_DRIVERS = [
  { value: 'sqlite', label: 'SQLite (Embedded, no server required)' },
  { value: 'postgres', label: 'PostgreSQL' },
  { value: 'mysql', label: 'MySQL' },
  { value: 'sqlserver', label: 'SQL Server' },
] as const;

const DEFAULT_PORTS: Record<string, number> = {
  postgres: 5432,
  mysql: 3306,
  sqlserver: 1433,
};

const MIN_ENCRYPTION_KEY_LENGTH = 16;
const GENERATED_KEY_LENGTH = 32;
const MIN_PASSWORD_LENGTH = 6;
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

  const form = useForm({
    initialValues: {
      database_driver: 'sqlite',
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
      if (active === 0) {
        if (!values.database_driver) {
          return { database_driver: 'Database driver is required' };
        }
        if (values.database_driver !== 'sqlite') {
          const errors: Record<string, string | null> = {};
          if (!values.db_host) errors.db_host = 'Host is required';
          if (!values.db_port) errors.db_port = 'Port is required';
          if (!values.db_username) errors.db_username = 'Username is required';
          if (!values.db_name) errors.db_name = 'Database name is required';
          if (Object.keys(errors).length > 0) return errors;
        }
        return {};
      }
      if (active === 1) {
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
      if (active === 2) {
        return {
          admin_username: values.admin_username.length < 3 ? 'Username must be at least 3 characters' : null,
          admin_password: values.admin_password.length < MIN_PASSWORD_LENGTH ? `Password must be at least ${MIN_PASSWORD_LENGTH} characters` : null,
          admin_full_name: !values.admin_full_name ? 'Full name is required' : null,
          admin_public_name: !values.admin_public_name ? 'Public name is required' : null,
          admin_email: !/^\S+@\S+$/.test(values.admin_email) ? 'Invalid email' : null,
        };
      }
      if (active === 3) {
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
    const port = DEFAULT_PORTS[value];
    if (port) {
      form.setFieldValue('db_port', port);
    }
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
      const message = err instanceof Error ? err.message : 'Connection test failed';
      setConnectionResult({ success: false, message });
    } finally {
      setTestingConnection(false);
    }
  }, [form.values]);

  const nextStep = () => {
    const validation = form.validate();
    if (!validation.hasErrors) {
      setActive((current) => (current < 5 ? current + 1 : current));
    }
  };

  const prevStep = () => setActive((current) => (current > 0 ? current - 1 : current));

  const handleSetup = async () => {
    setLoading(true);
    setError(null);
    try {
      const { err } = await processService.setup(form.values);
      if (err) {
        setError(err);
      } else {
        setActive(5);
      }
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : 'Setup failed';
      setError(message);
    } finally {
      setLoading(false);
    }
  };

  const selectedDriver = form.values.database_driver;
  const isSQLite = selectedDriver === 'sqlite';
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
      <Container size="md" w="100%">
        <Paper shadow="xl" radius="lg" p={40} withBorder>
          <Stack gap="xl">
            <Center>
              <Group gap="sm">
                <ThemeIcon size={48} radius="md" variant="gradient" gradient={{ from: 'blue', to: 'cyan' }}>
                  <Workflow size={28} />
                </ThemeIcon>
                <div>
                  <Title order={2} fw={900} lts={-0.5}>Hermod</Title>
                  <Text size="xs" c="dimmed" fw={700} tt="uppercase">System Setup Wizard</Text>
                </div>
              </Group>
            </Center>

            <Stepper active={active} onStepClick={setActive} allowNextStepsSelect={false} size="sm">
              {/* Step 0: Database Configuration */}
              <Stepper.Step
                label="Database"
                description="Connection settings"
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
                  {isSQLite && (
                    <Alert variant="light" color="blue" icon={<Database size={16} />}>
                      SQLite uses a local file (<strong>gobpm.db</strong>) and requires no additional configuration.
                      You can optionally specify a custom file path below.
                    </Alert>
                  )}
                  {isSQLite && (
                    <TextInput
                      label="Database File Path (optional)"
                      placeholder="gobpm.db"
                      description="Leave empty to use the default gobpm.db file"
                      {...form.getInputProps('db_name')}
                    />
                  )}
                  {!isSQLite && (
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
                          placeholder={String(DEFAULT_PORTS[selectedDriver] || 5432)}
                          required
                          min={1}
                          max={65535}
                          {...form.getInputProps('db_port')}
                        />
                      </Group>
                      <Group grow>
                        <TextInput
                          label="Username"
                          placeholder="gobpm"
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
                        placeholder="gobpm"
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
                description="Encryption key"
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
                        <ActionIcon aria-label="Refresh"
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
                            <ActionIcon aria-label="Confirm"
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
                        <ActionIcon aria-label="Refresh"
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
                            <ActionIcon aria-label="Confirm"
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
                        <ActionIcon aria-label="Refresh"
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
                            <ActionIcon aria-label="Confirm"
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
                <Stack align="center" gap="md" mt="xl" py="xl">
                  <ThemeIcon size={80} radius={100} color="green" variant="light">
                    <CheckCircle2 size={50} />
                  </ThemeIcon>
                  <Title order={2}>Setup Complete!</Title>
                  <Text ta="center" c="dimmed">
                    Hermod BPM has been successfully initialized.<br />
                    Your configuration has been saved to <strong>config.yaml</strong> with encrypted credentials.<br />
                    You can now log in with your administrator account.
                  </Text>
                  <Button size="lg" mt="md" onClick={onComplete} rightSection={<Rocket size={18} />}>
                    Get Started
                  </Button>
                </Stack>
              </Stepper.Completed>
            </Stepper>

            {error && (
              <Alert icon={<AlertCircle size={16} />} title="Setup Error" color="red" variant="light">
                {error}
              </Alert>
            )}

            {active < 5 && (
              <Group justify="flex-end" mt="xl">
                {active !== 0 && (
                  <Button variant="default" onClick={prevStep}>
                    Back
                  </Button>
                )}
                {active < 4 ? (
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
