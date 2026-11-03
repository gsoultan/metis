import { Menu, Text, UnstyledButton } from '@mantine/core';
import { useNavigate } from '@tanstack/react-router';
import { Building2, Check, ChevronDown, Plus, Settings2 } from 'lucide-react';
import { useSwitchOrganization } from '../../hooks/useOrganization';
import { useAppStore } from '../../store/useAppStore';
import classes from './AppHeader.module.css';

interface OrganizationPickerProps {
  organizations: Array<{ id: string; name: string }>;
}

/**
 * Which organization is being worked in, and the way to another.
 *
 * It was a select that appeared only for somebody in two organizations or
 * more, so with one there was nothing on screen saying that work happens
 * inside an organization at all, nor a way to make a second one. It now always
 * shows, and offers the organizations page and a new organization beside the
 * list.
 */
export function OrganizationPicker({ organizations }: OrganizationPickerProps) {
  const currentOrganizationId = useAppStore((state) => state.currentOrganizationId);
  const switchOrganization = useSwitchOrganization();
  const navigate = useNavigate();
  const current = organizations.find((o) => o.id === currentOrganizationId);

  return (
    <Menu position="bottom-start" width={280} withinPortal shadow="md">
      <Menu.Target>
        <UnstyledButton className={classes.organizationButton} aria-label={`Organization: ${current?.name ?? 'none chosen'}`}>
          <Building2 size={15} aria-hidden />
          <Text size="sm" fw={600} truncate>
            {current?.name ?? 'Choose an organization'}
          </Text>
          <ChevronDown size={14} aria-hidden style={{ marginLeft: 'auto', flex: 'none' }} />
        </UnstyledButton>
      </Menu.Target>
      <Menu.Dropdown>
        <Menu.Label>Your organizations</Menu.Label>
        {organizations.map((organization) => {
          const selected = organization.id === currentOrganizationId;
          return (
            <Menu.Item
              key={organization.id}
              onClick={() => switchOrganization(organization.id)}
              rightSection={selected ? <Check size={15} aria-hidden /> : null}
              aria-current={selected ? 'true' : undefined}
              fw={selected ? 600 : undefined}
            >
              {organization.name || organization.id}
            </Menu.Item>
          );
        })}
        <Menu.Divider />
        <Menu.Item onClick={() => void navigate({ to: '/organizations' })} leftSection={<Settings2 size={15} aria-hidden />}>
          Manage organizations
        </Menu.Item>
        <Menu.Item
          onClick={() => void navigate({ to: '/organizations', search: { new: true } })}
          leftSection={<Plus size={15} aria-hidden />}
          c="indigo"
        >
          New organization
        </Menu.Item>
      </Menu.Dropdown>
    </Menu>
  );
}
