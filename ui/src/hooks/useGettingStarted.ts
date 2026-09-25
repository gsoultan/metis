import { gettingStartedProgress, type GettingStartedProgress } from '../domain/gettingStarted';
import { useConnectorInstances } from './useConnectors';
import { useDefinitions } from './useDefinitions';
import { useInstances, useProcessStatistics } from './useInstances';
import { useParticipants } from './useParticipants';

/**
 * What the current project has done of getting started, for the Help drawer
 * and the Dashboard alike, so the two cannot disagree.
 *
 * The queries and their arguments are the Dashboard's own, so on the
 * Dashboard they are answered from its cache rather than asked for twice. Each
 * query goes to gettingStartedProgress whole, placeholder and error flags and
 * all; picking `data` out here is how another project's rows came to be
 * counted.
 */
export function useGettingStartedProgress(): { progress: GettingStartedProgress; retry: () => void } {
  const definitions = useDefinitions();
  // Not live: this only asks whether any instance exists, and a live list
  // polls every few seconds. The key is the Dashboard's, which is live.
  const instances = useInstances(1, 25, {}, false);
  const statistics = useProcessStatistics();
  const connections = useConnectorInstances();
  const people = useParticipants();

  const progress = gettingStartedProgress({ definitions, instances, statistics, connections, people });
  // Asks again for whatever failed, whether it threw or answered with an error.
  const retry = () => {
    for (const query of [definitions, instances, statistics, connections, people]) {
      if (query.isError || query.data?.err) void query.refetch();
    }
  };
  return { progress, retry };
}
