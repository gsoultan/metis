import { Tabs, Button, Stack } from '@mantine/core';
import { Network, Table2, Plus } from 'lucide-react';
import { PageHeader } from '../components/PageHeader';
import { DefinitionList } from './DefinitionList';
import { DecisionList } from './DecisionList';
import { CreationWizard } from '../components/CreationWizard';
import { useNavigate } from '@tanstack/react-router';
import { Route } from '../routes/_authenticated.models';
import { useState } from 'react';

export function Models() {
  const navigate = useNavigate({ from: Route.fullPath });
  const { tab } = Route.useSearch();
  const [wizardOpened, setWizardOpened] = useState(false);

  const setActiveTab = (val: string | null) => {
    navigate({ search: { tab: val === 'decisions' ? 'decisions' : 'processes' } });
  };

  const handleEditProcess = (id: string) => {
    navigate({
      to: '/designer',
      search: { definitionId: id }
    });
  };

  const handleEditDecision = (id: string) => {
    navigate({
      to: '/decision-editor',
      search: { id }
    });
  };

  return (
    <Stack gap="xl">
      <PageHeader 
        title="Models" 
        description="Design and manage your business processes and decision tables."
        actions={
          <Button 
            variant="filled" 
            color="indigo" 
            leftSection={<Plus size={16} />}
            onClick={() => setWizardOpened(true)}
          >
            Create New
          </Button>
        }
      />

      <Tabs value={tab} onChange={setActiveTab} variant="outline" radius="md">
        <Tabs.List mb="md">
          <Tabs.Tab value="processes" leftSection={<Network size={16} />}>
            Processes
          </Tabs.Tab>
          <Tabs.Tab value="decisions" leftSection={<Table2 size={16} />}>
            Decisions
          </Tabs.Tab>
        </Tabs.List>

        <Tabs.Panel value="processes">
          <DefinitionList hideHeader onEditModel={handleEditProcess} />
        </Tabs.Panel>
        <Tabs.Panel value="decisions">
          <DecisionList hideHeader onEdit={handleEditDecision} />
        </Tabs.Panel>
      </Tabs>

      <CreationWizard 
        opened={wizardOpened}
        onClose={() => setWizardOpened(false)}
        initialType={tab === 'processes' ? 'process' : 'decision'}
        onCreateProcess={(data) => {
          navigate({ to: '/designer', search: { name: data.name, key: data.key } });
        }}
        onCreateDecision={(data) => {
          navigate({ to: '/decision-editor', search: { name: data.name, key: data.key } });
        }}
      />
    </Stack>
  );
}
