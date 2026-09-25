import { gettingStartedProgress, type GettingStartedProgress } from '../domain/gettingStarted';
import { useConnectorInstances } from './useConnectors';
import { useDefinitions } from './useDefinitions';
import { useInstances, useProcessStatistics } from './useInstances';
import { useParticipants } from './useParticipants';

/**
 * What the current project has done of getting started, for the Help drawer
 * and the Dashboard alike, so the two cannot disagree.
 *
 * Each thing is asked for as a count or a single row where it can be: the
 * completed tasks from the project statistics, one person rather than the
 * directory. The definitions, instances and statistics are asked with the
 * Dashboard's own keys, so on the Dashboard they come from its cache rather
 * than being asked for twice.
 *
 * Each query goes to gettingStartedProgress whole, placeholder and error flags
 * and all; picking `data` out here is how another project's rows came to be
 * counted.
 */
export function useGettingStartedProgress(): { progress: GettingStartedProgress; retry: () => void } {
  const definitions = useDefinitions();
  // Not live: this only asks whether any instance exists, and a live list
  // polls every few seconds. The key is the Dashboard's, which is live.
  const instances = useInstances(1, 25, {}, false);
  const statistics = useProcessStatistics();
  const connections = useConnectorInstances();
  // Whether there is anybody, which is one row. The directory can be
  // thousands, and this is asked every time Help opens.
  const people = useParticipants({ limit: 1 });

  const progress = gettingStartedProgress({ definitions, instances, statistics, connections, people });
  // Asks again for whatever failed, whether it threw or answered with an error.
  const retry = () => {
    for (const query of [definitions, instances, statistics, connections, people]) {
      if (query.isError || query.data?.err) void query.refetch();
    }
  };
  return { progress, retry };
}
