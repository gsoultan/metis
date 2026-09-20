import { 
  Grid, 
  Card, 
  Text, 
  Group, 
  Stack, 
  ThemeIcon, 
  Title, 
  Button, 
  Badge, 
  rem, 
  Progress,
} from '@mantine/core';
import type { LucideIcon } from 'lucide-react';
import { 
  GitBranch, 
  TrendingUp, 
  Activity, 
  CheckCircle, 
  AlertCircle,
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
import { Link } from '@tanstack/react-router';
import { ComingSoonButton } from '../components/state/ComingSoon';
import { StatsLoadingState, ErrorState } from '../components/state';
import { useTranslation } from '../i18n/context';

/**
 * A single headline number.
 *
 * There is deliberately no `trend` prop. It used to accept a string, and every
 * call site passed a hardcoded one ("+12%", "+5%", "+2%") rendered beside a
 * green upward arrow and the words "vs last month" — while no endpoint in the
 * product computes a trend of any kind. Removing the prop means the fabrication
 * cannot come back without someone first building the data.
 *
 * `progress` is only for values that genuinely are a percentage of a whole.
 */
function StatCard({
  title,
  value,
  icon: Icon,
  color,
  progress,
  progressLabel,
  hint,
}: {
  title: string;
  value: React.ReactNode;
  icon: LucideIcon;
  color: string;
  progress?: number;
  progressLabel?: string;
  hint?: string;
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
            <Text size="xs" c="dimmed" fw={600}>{progressLabel ?? 'Complete'}</Text>
            <Text size="xs" fw={700} c={color}>{progress}%</Text>
          </Group>
          <Progress
            value={progress}
            color={color}
            size="sm"
            radius="xl"
            aria-label={`${progressLabel ?? 'Complete'}: ${progress}%`}
          />
        </Stack>
      )}

      {hint && (
        <Text size="xs" c="dimmed" mt="md">{hint}</Text>
      )}
    </Card>
  );
}

export function Dashboard() {
  const { t } = useTranslation();
  const { currentProjectId, currentOrganizationId } = useAppStore();
  const { data: statsData, isLoading: statsLoading, error: statsError, refetch: refetchStats } = useProcessStatistics();
  const { data: defs } = useDefinitions();
  const { data: projectsData } = useProjects(currentOrganizationId);
  const { data: instancesData } = useInstances();
  

  // Falling back to zeros made an unloaded dashboard indistinguishable from a
  // real, idle one — "we don't know yet" rendered as "we know, and it's none".
  // The zeros remain only as a shape for the render below; statsLoading decides
  // whether they are ever shown.
  /*
   * These come from the Connect (protobuf) client, which serialises field
   * names in camelCase — activeInstances, not active_instances.
   *
   * Every read here used snake_case, so each one evaluated to `undefined` and
   * the `|| 0` fallback rendered a zero. The dashboard showed 0 active
   * instances, 0 process models and 0% completion no matter what the system
   * was actually doing, and nothing caught it because the processService
   * facade was typed `any`.
   */
  const stats = statsData?.stats;
  const activeInstances = stats?.activeInstances ?? 0;
  const totalTasks = stats?.totalTasks ?? 0;
  const pendingTasks = stats?.pendingTasks ?? 0;

  /*
   * Deliberately NOT stats.failedInstances, which counts instances whose
   * *status* is FAILED. A BPMN instance whose job exhausts its retries does not
   * go FAILED — it stays RUNNING and raises an incident. So that counter read 0
   * while four instances sat stuck on a dead HTTP call, and this card told the
   * operator "Nothing has failed" on their first screen.
   *
   * `needsAttentionTotal` is what the Instances page already filters on, so the
   * dashboard and that page now answer the same question with the same number.
   */
  const needsAttention = instancesData?.needsAttentionTotal ?? 0;

  const lastInstanceId = instancesData?.instances?.[0]?.id;

  /*
   * Distinct process keys, not rows. `definitions` holds one row per deployed
   * *version*, so counting it said "8 process models" for the two processes the
   * Models page lists. Both numbers were defensible and a user reading them an
   * hour apart could not reconcile them.
   */
  const totalDefinitions = new Set((defs?.definitions ?? []).map((d) => d.key)).size;
  const totalProjects = projectsData?.projects?.length || 0;


  if (!currentProjectId) {
    return (
      <Stack gap="xl">
        <PageHeader 
          title={t('page.welcome.title')}
          description={t('page.welcome.subtitle')}
        />
        
        <Card shadow="sm" radius="lg" withBorder py={60}>
          <Stack align="center" gap="md">
            <ThemeIcon size={80} radius="xl" variant="light" color="indigo">
              <TrendingUp size={40} />
            </ThemeIcon>
            <Title order={2}>{t('dash.readyTitle')}</Title>
            {/*
              A project is now chosen automatically, so this is reached when
              there is none to choose rather than because somebody skipped a
              step. Telling them to "select one from the header" when the header
              is empty was the old, unhelpful half of this.
            */}
            <Text c="dimmed" ta="center" maw={500}>
              {totalProjects > 0
                ? t('dash.readyLoading')
                : t('dash.readyNoProjects')}
            </Text>

            {totalProjects === 0 && (
              <Button component={Link} to="/projects" size="md" radius="md" color="indigo">
                {t('dash.createFirstProject')}
              </Button>
            )}
          </Stack>
        </Card>
      </Stack>
    );
  }

  const completionRate = totalTasks > 0 
    ? Math.round(((totalTasks - pendingTasks) / totalTasks) * 100) 
    : 0;

  return (
    <Stack gap="xl">
      <PageHeader 
        title={t('page.dashboard.title')}
        description={t('page.dashboard.subtitle')}
        actions={
          <ComingSoonButton variant="light" leftSection={<Activity size={16} />} label="Report export is not implemented yet">
            {t('dash.generateReport')}
          </ComingSoonButton>
        }
      />

      {statsLoading ? (
        <StatsLoadingState count={4} />
      ) : statsError ? (
        <ErrorState error={statsError} action="load your statistics" onRetry={() => refetchStats()} />
      ) : (
      <Grid gap="xl">
        <Grid.Col span={{ base: 12, md: 3 }}>
          <StatCard
            title={t('dash.activeInstances')}
            value={activeInstances}
            icon={Activity}
            color="indigo"
            hint={t('dash.activeInstancesHint')}
          />
        </Grid.Col>
        <Grid.Col span={{ base: 12, md: 3 }}>
          <StatCard
            title={t('dash.processModels')}
            value={totalDefinitions}
            icon={GitBranch}
            color="teal"
            hint={t('dash.processModelsHint')}
          />
        </Grid.Col>
        <Grid.Col span={{ base: 12, md: 3 }}>
          <StatCard
            title={t('dash.tasksCompleted')}
            value={`${completionRate}%`}
            icon={CheckCircle}
            color="orange"
            progress={completionRate}
            progressLabel={t('dash.tasksProgress', { done: totalTasks - pendingTasks, total: totalTasks })}
          />
        </Grid.Col>
        <Grid.Col span={{ base: 12, md: 3 }}>
          <StatCard
            title={t('dash.needsAttention')}
            value={needsAttention}
            icon={AlertCircle}
            color={needsAttention > 0 ? 'red' : 'green'}
            hint={
              needsAttention > 0
                ? t('dash.needsAttentionSome')
                : t('dash.needsAttentionNone')
            }
          />
        </Grid.Col>
      </Grid>
      )}

      <Grid gap="xl">
        <Grid.Col span={12}>
          <Card shadow="sm" radius="lg" withBorder h="100%">
            <Group justify="space-between" mb="xl">
              <Group gap="sm">
                <Title order={4}>{t('dash.timeline')}</Title>
                <Badge variant="light" color="indigo" radius="sm">{t('dash.recentActivity')}</Badge>
              </Group>
              <Button component={Link} to="/instances" variant="subtle" size="xs">{t('dash.viewAllInstances')}</Button>
            </Group>
            
            {lastInstanceId ? (
              <BusinessTimeline instanceId={lastInstanceId} />
            ) : (
              <Stack align="center" py={60} gap="sm">
                <ThemeIcon size={60} radius="xl" variant="light" color="gray">
                  <Activity size={32} />
                </ThemeIcon>
                <Text fw={700}>{t('dash.noActivity')}</Text>
                <Text size="sm" c="dimmed">{t('dash.noActivityHint')}</Text>
              </Stack>
            )}
          </Card>
        </Grid.Col>
        
      </Grid>


      {/*
        The "Starter Templates" section was removed, not disabled.

        It offered three cards badged "Recommended" whose every button was a
        ComingSoonButton, because process templates are not implemented. That is
        prime dashboard space advertising a feature that cannot be used: a first-
        time user clicks all three and concludes the product is broken.

        A disabled control teaches when the feature exists and is unavailable to
        *you*. It misleads when the feature does not exist at all. Bring this back
        with the templates, not before.
      */}
    </Stack>
  );
}
