import {
  Title,
  Text,
  Paper,
  Stack,
  Group,
  Avatar,
  Divider,
  SimpleGrid,
  ThemeIcon,
} from '@mantine/core';
import { Mail, Building2 } from 'lucide-react';
import { useState } from 'react';
import { ChangePasswordModal } from '../components/ChangePasswordModal';
import { ProfileForm } from '../components/ProfileForm';
import { DetailLoadingState, ErrorState } from '../components/state';
import { useAppStore } from '../store/useAppStore';
import { PageHeader } from '../components/PageHeader';
import { useOwnProfile } from '../hooks/useUser';
import { roleLabels } from '../domain/roles';
import { useTranslation } from '../i18n/context';

export function Profile() {
  const { t } = useTranslation();
  const user = useAppStore((state) => state.user);
  // The session knows the names; the email is only on the account, so the
  // form waits for the account rather than start from a guess.
  const ownProfile = useOwnProfile();
  const profile = ownProfile.data?.user;
  const [changingPassword, setChangingPassword] = useState(false);

  if (!user) return null;

  return (
    <Stack gap="xl">
      <PageHeader
        title={t('page.profile.title')}
        description={t('page.profile.subtitle')}
      />

      <SimpleGrid cols={{ base: 1, md: 3 }} spacing="xl">
        <Stack gap="xl" style={{ gridColumn: 'span 1' }}>
          <Paper p="xl" radius="lg" withBorder shadow="sm" ta="center">
            <Avatar
              size={120}
              radius={120}
              mx="auto"
              color="blue"
              variant="light"
            >
              {user.displayName?.charAt(0) || user.name?.charAt(0) || 'U'}
            </Avatar>
            <Title order={2} size="h3" mt="md">{user.displayName || user.name}</Title>
            <Text c="dimmed" size="sm">{roleLabels(user.role)}</Text>

            <Divider my="lg" />

            <Stack gap="sm">
              <Group gap="sm" wrap="nowrap">
                <ThemeIcon variant="light" color="gray" size="sm">
                  <Mail size={14} />
                </ThemeIcon>
                {/* The username stood here as the email address. */}
                <Text size="xs" truncate>{profile !== undefined && (profile.email || 'No email on file')}</Text>
              </Group>
              {/*
                A join date and a location were hardcoded here ("Joined March
                2024", "Berlin, Germany"). Neither is stored on the user, so
                every account displayed the same fictional biography.
              */}
            </Stack>
          </Paper>

          <Paper p="xl" radius="lg" withBorder shadow="sm">
            <Title order={3} size="h5" mb="md">Your Organizations</Title>
            <Stack gap="sm">
              {user.organizations?.map((org) => (
                <Paper key={org.id} withBorder p="xs" radius="md" bg="gray.0">
                  <Group justify="space-between">
                    <Group gap="xs">
                      <Building2 size={16} color="var(--mantine-color-blue-6)" />
                      <Text size="sm" fw={600}>{org.name}</Text>
                    </Group>
                  </Group>
                </Paper>
              ))}
            </Stack>
          </Paper>
        </Stack>

        <Paper p="xl" radius="lg" withBorder shadow="sm" style={{ gridColumn: 'span 2' }}>
          {profile !== undefined && (
            <ProfileForm key={profile.id} profile={profile} onChangePassword={() => setChangingPassword(true)} />
          )}
          {profile === undefined && ownProfile.isPending && <DetailLoadingState />}
          {profile === undefined && !ownProfile.isPending && (
            <ErrorState error={ownProfile.error} action="load your profile" onRetry={() => void ownProfile.refetch()} />
          )}
        </Paper>
      </SimpleGrid>

      <ChangePasswordModal opened={changingPassword} onClose={() => setChangingPassword(false)} />
    </Stack>
  );
}
