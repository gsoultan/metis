import { viewerAccess, type ViewerAccess } from '../domain/organizationRoles';
import { useAppStore } from '../store/useAppStore';
import { useOwnProfile } from './useUser';

/**
 * What the signed-in person may do in the organization being worked in: the
 * roles they hold here and in every organization, and whether they may change
 * the roles held in every organization.
 *
 * Read from their own account, which the server answers for the organization
 * the request is for; until it has, from the roles the sign-in listed. Derived
 * on every render, so nothing here can fall out of step with either.
 */
export function useViewerAccess(): ViewerAccess {
  const signedIn = useAppStore((state) => state.user);
  const ownProfile = useOwnProfile();
  return viewerAccess(signedIn, ownProfile.data);
}
