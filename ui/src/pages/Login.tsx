import { 
  TextInput, 
  PasswordInput, 
  Paper, 
  Title, 
  Button, 
  Alert,
  Stack,
  Text,
  Center,
  Box,
  ThemeIcon,
  Divider,
  Group,
} from '@mantine/core';
import { useState } from 'react';
import { useNavigate } from '@tanstack/react-router';
import { useAppStore } from '../store/useAppStore';
import { processService } from '../services/api';
import { 
  AlertCircle, 
  ShieldCheck, 
  Activity, 
  Workflow, 
  ArrowRight, 
  Zap, 
  Lock,
  GitBranch,
  BarChart3,
} from 'lucide-react';
import { toStoreUser } from '../mappers/userMapper';

const BRAND_FEATURES = [
  { icon: ShieldCheck, title: 'Enterprise Security', desc: 'Role-based access control & full audit trail' },
  { icon: Zap, title: 'Lightning Fast', desc: 'Sub-millisecond process execution engine' },
  { icon: Activity, title: 'Real-time Insight', desc: 'Live dashboards & process monitoring' },
  { icon: Lock, title: 'Compliance Ready', desc: 'GDPR-compliant with immutable logs' },
  { icon: GitBranch, title: 'Visual Modeling', desc: 'Drag-and-drop BPMN process designer' },
  { icon: BarChart3, title: 'Analytics', desc: 'KPI tracking & bottleneck detection' },
] as const;

export function Login({ redirectTo }: { redirectTo?: string }) {
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const setAuth = useAppStore((state) => state.setAuth);
  const navigate = useNavigate();

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setLoading(true);
    setError(null);

    try {
      const { user, token, err } = await processService.login(username, password);
      
      if (err) {
        setError(err.message || 'Login failed');
      } else if (user && token) {
        setAuth(toStoreUser(user), token);
        await navigate({ to: redirectTo || '/' });
      } else {
        setError('Invalid response from server');
      }
    } catch (err: any) {
      setError(err.message || 'Failed to login');
    } finally {
      setLoading(false);
    }
  };

  return (
    <Box className="login-root">
      {/* Branding Column */}
      <Box className="login-brand" visibleFrom="md">
        {/* Animated background orbs */}
        <Box className="login-orb login-orb--1" />
        <Box className="login-orb login-orb--2" />
        <Box className="login-orb login-orb--3" />

        {/* Grid pattern overlay */}
        <Box className="login-grid-overlay" />

        <Stack gap={48} className="login-brand-content animate-slide-up">
          {/* Logo + Title */}
          <Group gap="lg" align="center">
            <ThemeIcon 
              size={72} 
              radius="xl" 
              variant="white"
              style={{ 
                background: 'rgba(255,255,255,0.15)',
                backdropFilter: 'blur(12px)',
                border: '1px solid rgba(255,255,255,0.2)',
                boxShadow: '0 8px 32px rgba(0,0,0,0.15)',
              }}
            >
              <Workflow size={40} color="white" />
            </ThemeIcon>
            <div>
              <Title order={1} fw={900} size={44} c="white" style={{ letterSpacing: '-0.04em' }}>
                Metis
              </Title>
              <Text size="sm" c="rgba(255,255,255,0.6)" fw={500} mt={-4}>
                Business Process Management
              </Text>
            </div>
          </Group>
          
          {/* Headline */}
          <Stack gap="sm" className="animate-slide-up animation-delay-100">
            <Title order={2} fw={800} size={48} lh={1.1} c="white" style={{ letterSpacing: '-0.03em' }}>
              Orchestrate.{' '}
              <Text 
                component="span" 
                inherit 
                style={{ 
                  background: 'linear-gradient(135deg, #60a5fa 0%, #34d399 50%, #a78bfa 100%)',
                  WebkitBackgroundClip: 'text',
                  WebkitTextFillColor: 'transparent',
                }}
              >
                Automate.
              </Text>
              <br />
              Accelerate.
            </Title>
            <Text size="lg" c="rgba(255,255,255,0.65)" maw={460} lh={1.7} mt="sm">
              The modern BPM platform for teams that move fast. 
              Design, deploy, and monitor workflows with confidence.
            </Text>
          </Stack>

          {/* Feature Grid */}
          <Box 
            className="animate-slide-up animation-delay-200"
            style={{ 
              display: 'grid', 
              gridTemplateColumns: '1fr 1fr', 
              gap: '20px',
              marginTop: 8,
            }}
          >
            {BRAND_FEATURES.map((item, i) => (
              <Group 
                key={i} 
                gap="sm" 
                wrap="nowrap" 
                align="flex-start"
                className="login-feature-item"
                style={{ animationDelay: `${0.3 + i * 0.08}s` }}
              >
                <ThemeIcon 
                  size={36} 
                  radius="md" 
                  variant="light"
                  style={{ 
                    background: 'rgba(255,255,255,0.1)',
                    border: '1px solid rgba(255,255,255,0.1)',
                    flexShrink: 0,
                  }}
                >
                  <item.icon size={18} color="rgba(255,255,255,0.85)" />
                </ThemeIcon>
                <div>
                  <Text fw={700} size="sm" c="white">{item.title}</Text>
                  <Text size="xs" c="rgba(255,255,255,0.5)" lh={1.4}>{item.desc}</Text>
                </div>
              </Group>
            ))}
          </Box>
        </Stack>

        <Text size="xs" c="rgba(255,255,255,0.3)" className="login-brand-footer">
          © 2026 Metis BPM · Enterprise Edition
        </Text>
      </Box>

      {/* Form Column */}
      <Box className="login-form-col">
        <Center h="100%" p="xl">
          <Box w="100%" maw={400} className="animate-fade-in animation-delay-200">
            <Stack gap={36}>
              {/* Mobile Logo */}
              <Box hiddenFrom="md" ta="center">
                <ThemeIcon 
                  size={60} 
                  radius="xl" 
                  variant="gradient" 
                  gradient={{ from: '#4f46e5', to: '#7c3aed' }} 
                  mb="md" 
                  mx="auto"
                >
                  <Workflow size={32} />
                </ThemeIcon>
                <Title order={2} fw={900}>Metis</Title>
              </Box>

              {/* Welcome Text */}
              <div>
                <Title 
                  order={2} 
                  fw={800} 
                  size={28} 
                  ta={{ base: 'center', md: 'left' }} 
                  style={{ letterSpacing: '-0.02em' }}
                >
                  Welcome back
                </Title>
                <Text c="dimmed" size="md" mt={6} ta={{ base: 'center', md: 'left' }}>
                  Sign in to continue to your workspace
                </Text>
              </div>

              {/* Login Form */}
              <Paper 
                p={32} 
                radius="lg" 
                className="login-card"
              >
                <form onSubmit={handleSubmit}>
                  <Stack gap="md">
                    {error && (
                      <Alert 
                        icon={<AlertCircle size={18} />} 
                        title="Authentication Failed" 
                        color="red" 
                        radius="md" 
                        variant="light"
                      >
                        {error}
                      </Alert>
                    )}
                    
                    <TextInput 
                      label="Username" 
                      placeholder="Enter your username" 
                      required 
                      size="md"
                      radius="md"
                      value={username}
                      onChange={(e) => setUsername(e.currentTarget.value)}
                      styles={{
                        label: { fontWeight: 600, marginBottom: 6, fontSize: 13 },
                        input: { 
                          border: '1.5px solid var(--mantine-color-gray-3)',
                          transition: 'border-color 0.2s ease, box-shadow 0.2s ease',
                        },
                      }}
                    />
                    <PasswordInput 
                      label="Password" 
                      placeholder="••••••••" 
                      required 
                      size="md"
                      radius="md"
                      value={password}
                      onChange={(e) => setPassword(e.currentTarget.value)}
                      styles={{
                        label: { fontWeight: 600, marginBottom: 6, fontSize: 13 },
                        input: { 
                          border: '1.5px solid var(--mantine-color-gray-3)',
                          transition: 'border-color 0.2s ease, box-shadow 0.2s ease',
                        },
                      }}
                    />
                    
                    <Button 
                      type="submit" 
                      fullWidth 
                      loading={loading} 
                      size="lg" 
                      mt="xs" 
                      radius="md"
                      rightSection={<ArrowRight size={18} />}
                      className="login-submit-btn"
                    >
                      Sign in
                    </Button>
                  </Stack>
                </form>
              </Paper>
              
              {/* Test Credentials */}
              <Stack gap={6} ta="center">
                <Divider label="Demo Credentials" labelPosition="center" color="gray.3" />
                <Group justify="center" gap={6}>
                  <Text size="xs" c="dimmed">Username:</Text>
                  <Text size="xs" fw={700} ff="monospace" c="indigo">admin</Text>
                  <Text size="xs" c="dimmed" ml={4}>Password:</Text>
                  <Text size="xs" fw={700} ff="monospace" c="indigo">admin</Text>
                </Group>
              </Stack>
            </Stack>
          </Box>
        </Center>
      </Box>
    </Box>
  );
}
