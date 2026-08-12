import { 
  Grid, 
  Card, 
  Text, 
  Group, 
  Stack, 
  ThemeIcon, 
  Box, 
  Title, 
  Button, 
  Divider, 
  Badge, 
  rem, 
  Progress,
  Modal,
} from '@mantine/core';
import { useDisclosure } from '@mantine/hooks';
import { 
  GitBranch, 
  Activity, 
  CheckCircle, 
  TrendingUp, 
  AlertCircle,
  Clock,
  LayoutGrid,
} from 'lucide-react';
import { 
  useDefinitions, 
  useProjects,
  useProcessStatistics,
  useInstances,
} from '../hooks/useProcess';
import { useAppStore } from '../store/useAppStore';
import { PageHeader } from '../components/PageHeader';
import { BusinessTimeline } from '../components/BusinessTimeline';
import { BPMNGraph } from '../components/BPMNGraph';
import { useState } from 'react';

function StatCard({ 
  title, 
  value, 
  icon: Icon, 
  color, 
  trend, 
  progress 
}: { 
  title: string, 
  value: any, 
  icon: any, 
  color: string, 
  trend?: string,
  progress?: number 
}) {
  return (
    <Card shadow="md" radius="lg">
      <Group justify="space-between" align="flex-start" mb="sm">
        <Stack gap={0}>
          <Text size="xs" c="dimmed" fw={700} tt="uppercase" lts={rem(1)}>
            {title}
          </Text>
          <Text size="xl" fw={800} mt={5} style={{ fontSize: rem(28) }}>
            {value}
          </Text>
        </Stack>
        <ThemeIcon size={rem(48)} radius="md" variant="light" color={color}>
          <Icon size={rem(24)} />
        </ThemeIcon>
      </Group>
      
      {progress !== undefined && (
        <Stack gap={4} mt="md">
          <Group justify="space-between" align="flex-end">
            <Text size="xs" c="dimmed" fw={600}>Target completion</Text>
            <Text size="xs" fw={700} c={color}>{progress}%</Text>
          </Group>
          <Progress value={progress} color={color} size="sm" radius="xl" />
        </Stack>
      )}

      {trend && (
        <Group gap="xs" mt="md">
          <ThemeIcon size="xs" radius="xl" variant="transparent" color="green">
            <TrendingUp size={rem(14)} />
          </ThemeIcon>
          <Text size="xs" c="green" fw={700}>{trend}</Text>
          <Text size="xs" c="dimmed">vs last month</Text>
        </Group>
      )}
    </Card>
  );
}

export function Dashboard() {
  const { currentProjectId, setActiveTab, currentOrganizationId } = useAppStore();
  const { data: statsData } = useProcessStatistics();
  const { data: defs } = useDefinitions();
  const { data: projectsData } = useProjects(currentOrganizationId);
  const { data: instancesData } = useInstances();
  
  const [heatmapOpened, { open: openHeatmap, close: closeHeatmap }] = useDisclosure(false);
  const [selectedHeatmapDef, setSelectedHeatmapDef] = useState<any>(null);

  const stats = statsData?.stats || {
    active_instances: 0,
    completed_instances: 0,
    failed_instances: 0,
    total_tasks: 0,
    pending_tasks: 0,
    node_frequencies: {}
  };

  const lastInstanceId = instancesData?.instances?.[0]?.id;

  const totalDefinitions = defs?.definitions?.length || 0;
  const totalProjects = projectsData?.projects?.length || 0;

  const nodeFreqs = stats.node_frequencies || {};
  const topNodes = Object.entries(nodeFreqs)
    .sort(([, a], [, b]) => (b as number) - (a as number))
    .slice(0, 5);

  if (!currentProjectId) {
    return (
      <Stack gap="xl">
        <PageHeader 
          title="Welcome to Hermod BPM" 
          description="Get started by selecting or creating a project."
        />
        
        <Card shadow="sm" radius="lg" withBorder py={60}>
          <Stack align="center" gap="md">
            <ThemeIcon size={80} radius="xl" variant="light" color="indigo">
              <TrendingUp size={40} />
            </ThemeIcon>
            <Title order={2}>Ready to automate?</Title>
            <Text c="dimmed" ta="center" maw={500}>
              You haven't selected a project yet. Projects allow you to group related process models and tasks together.
            </Text>
            
            {totalProjects > 0 ? (
              <Text fw={700}>Select an existing project from the header to see its dashboard.</Text>
            ) : (
              <Button size="md" radius="md" color="indigo" onClick={() => setActiveTab('projects')}>
                Create your first project
              </Button>
            )}
          </Stack>
        </Card>
      </Stack>
    );
  }

  const completionRate = stats.total_tasks > 0 
    ? Math.round(((stats.total_tasks - stats.pending_tasks) / stats.total_tasks) * 100) 
    : 0;

  return (
    <Stack gap="xl">
      <PageHeader 
        title="Dashboard" 
        description="Overview of your business processes and tasks."
        actions={
          <Button variant="light" leftSection={<Activity size={16} />}>
            Generate Report
          </Button>
        }
      />

      <Grid gap="xl">
        <Grid.Col span={{ base: 12, md: 3 }}>
          <StatCard 
            title="Active Instances" 
            value={stats.active_instances} 
            icon={Activity} 
            color="indigo" 
            trend="+12%"
            progress={75}
          />
        </Grid.Col>
        <Grid.Col span={{ base: 12, md: 3 }}>
          <StatCard 
            title="Process Models" 
            value={totalDefinitions} 
            icon={GitBranch} 
            color="teal" 
            progress={100}
          />
        </Grid.Col>
        <Grid.Col span={{ base: 12, md: 3 }}>
          <StatCard 
            title="Task Completion" 
            value={`${completionRate}%`} 
            icon={CheckCircle} 
            color="orange" 
            trend="+5%"
            progress={completionRate}
          />
        </Grid.Col>
        <Grid.Col span={{ base: 12, md: 3 }}>
          <StatCard 
            title="SLA Compliance" 
            value="94%" 
            icon={Clock} 
            color="green" 
            trend="+2%"
            progress={94}
          />
        </Grid.Col>
      </Grid>

      <Grid gap="xl">
        <Grid.Col span={{ base: 12, md: 8 }}>
          <Card shadow="sm" radius="lg" withBorder h="100%">
            <Group justify="space-between" mb="xl">
              <Group gap="sm">
                <Title order={4}>Business Timeline</Title>
                <Badge variant="light" color="indigo" radius="sm">Recent Activity</Badge>
              </Group>
              <Button variant="subtle" size="xs">View All Logs</Button>
            </Group>
            
            {lastInstanceId ? (
              <BusinessTimeline instanceId={lastInstanceId} />
            ) : (
              <Stack align="center" py={60} gap="sm">
                <ThemeIcon size={60} radius="xl" variant="light" color="gray">
                  <Activity size={32} />
                </ThemeIcon>
                <Text fw={700}>No recent activity</Text>
                <Text size="sm" c="dimmed">Start a process to see the activity timeline here.</Text>
              </Stack>
            )}
          </Card>
        </Grid.Col>
        
        <Grid.Col span={{ base: 12, md: 4 }}>
          <Card shadow="sm" radius="lg" withBorder h="100%">
            <Title order={4} mb="lg">Most Active Nodes</Title>
            <Stack gap="md">
              {topNodes.length === 0 ? (
                <Text size="sm" c="dimmed">No activity data yet.</Text>
              ) : (
                topNodes.map(([nodeId, count]) => (
                  <Box key={nodeId}>
                    <Group justify="space-between" mb={4}>
                      <Text size="sm" fw={700} lineClamp={1}>{nodeId}</Text>
                      <Badge variant="light" color="orange">{(count as number)} hits</Badge>
                    </Group>
                    <Progress 
                      value={Math.min(((count as number) / (stats.completed_instances || 1)) * 100, 100)} 
                      color="orange" 
                      size="xs" 
                      radius="xl" 
                    />
                  </Box>
                ))
              )}
              <Divider my="sm" />
              <Stack gap="xs">
                <Text size="xs" fw={700} c="dimmed" tt="uppercase">Heatmap Overlays</Text>
                {defs?.definitions?.slice(0, 3).map((def: any) => (
                  <Button 
                    key={def.id}
                    variant="light" 
                    size="xs"
                    leftSection={<LayoutGrid size={14} />} 
                    onClick={() => {
                      setSelectedHeatmapDef(def);
                      openHeatmap();
                    }}
                  >
                    {def.name}
                  </Button>
                ))}
              </Stack>
            </Stack>
          </Card>
        </Grid.Col>
      </Grid>

      <Modal
        opened={heatmapOpened}
        onClose={closeHeatmap}
        title={<Group gap="xs"><Activity size={20} color="orange" /><Text fw={800}>Process Heatmap: {selectedHeatmapDef?.name}</Text></Group>}
        size="90%"
        radius="lg"
      >
        <Box h={600} style={{ position: 'relative' }}>
          {selectedHeatmapDef && (
            <BPMNGraph 
              nodes={selectedHeatmapDef.nodes} 
              flows={selectedHeatmapDef.flows}
              isReadOnly 
              heatmapData={stats.node_frequencies}
            />
          )}
        </Box>
      </Modal>

      <Card shadow="sm" radius="lg" withBorder mb="xl">
        <Group justify="space-between" mb="xl">
          <Group gap="sm">
            <ThemeIcon color="yellow" variant="light" size="lg">
              <GitBranch size={20} />
            </ThemeIcon>
            <Title order={4}>Starter Templates</Title>
            <Badge variant="light" color="yellow">Recommended</Badge>
          </Group>
          <Text size="xs" c="dimmed">Quick start by picking a template</Text>
        </Group>
        
        <Grid gap="md">
          {[
            { 
              title: "Simple Approval", 
              desc: "A basic two-step approval process with conditional branching.", 
              color: "green",
              icon: CheckCircle
            },
            { 
              title: "Support Ticket", 
              desc: "Escalate issues based on priority and notify stakeholders.", 
              color: "orange",
              icon: AlertCircle
            },
            { 
              title: "Invoice Processing", 
              desc: "Automate invoice verification and payment triggering.", 
              color: "blue",
              icon: Activity
            }
          ].map(t => (
            <Grid.Col span={{ base: 12, md: 4 }} key={t.title}>
              <Card 
                withBorder 
                padding="md" 
                radius="md" 
                style={{ 
                  cursor: 'pointer', 
                  height: '100%', 
                  transition: 'transform 0.2s',
                  '&:hover': { transform: 'translateY(-5px)' }
                }}
              >
                 <Stack align="center" ta="center" gap="sm">
                   <ThemeIcon variant="light" color={t.color} size={50} radius="xl">
                      <t.icon size={24} />
                   </ThemeIcon>
                   <Box>
                      <Text size="md" fw={700}>{t.title}</Text>
                      <Text size="xs" c="dimmed" mt={4}>{t.desc}</Text>
                   </Box>
                   <Button variant="light" color={t.color} size="xs" fullWidth mt="xs">
                      Use Template
                   </Button>
                 </Stack>
              </Card>
            </Grid.Col>
          ))}
        </Grid>
      </Card>
    </Stack>
  );
}
