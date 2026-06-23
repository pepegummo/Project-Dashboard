import { defineStore } from 'pinia';
import { ref, computed } from 'vue';
import { api } from '@/services/api.service';
import { wsService } from '@/services/ws.service';
import type { User, OrgOption } from '@/types';

// Decode a JWT's exp claim. Unparseable or past-due → expired.
function isExpired(t: string): boolean {
  try {
    const { exp } = JSON.parse(atob(t.split('.')[1]));
    return !exp || exp * 1000 <= Date.now();
  } catch {
    return true;
  }
}

function decodeOrgId(t: string): string | null {
  try { return JSON.parse(atob(t.split('.')[1])).orgId ?? null; }
  catch { return null; }
}

export const useAuthStore = defineStore('auth', () => {
  const user = ref<User | null>(null);
  const token = ref<string | null>(localStorage.getItem('auth_token'));
  const organizations = ref<OrgOption[]>(
    JSON.parse(localStorage.getItem('auth_orgs') ?? 'null') ?? []
  );
  const activeOrgId = ref<string | null>(token.value ? decodeOrgId(token.value) : null);
  const loading = ref(false);
  const error = ref<string | null>(null);

  // A token is "authenticated" only if it hasn't expired. Without this check a stale
  // token would route a refresh straight into the dashboard, which then 401s and bounces
  // back to /login — the brief dashboard flash.
  const isAuthenticated = computed(() => !!token.value && !isExpired(token.value));
  const isAdmin = computed(() => user.value?.role === 'admin');
  const isEditor = computed(() => ['admin', 'editor'].includes(user.value?.role ?? ''));

  async function login(email: string, password: string) {
    loading.value = true;
    error.value = null;
    try {
      const result = await api.login(email, password);
      token.value = result.token;
      user.value = result.user;
      organizations.value = result.organizations ?? [];
      localStorage.setItem('auth_orgs', JSON.stringify(organizations.value));
      activeOrgId.value = result.user.organizationId;
      // Multiple orgs → don't persist yet; hold the token in memory so switch-org
      // works, but a refresh before the user picks an org sends them back to login.
      // Single org → persist immediately (no picker step).
      const accessible = organizations.value.filter(o => o.isMember);
      if (accessible.length > 1) api.setOverrideToken(result.token);
      else api.setToken(result.token);
      wsService.connect(result.token);
      return true;
    } catch (err) {
      error.value = (err as Error).message;
      return false;
    } finally {
      loading.value = false;
    }
  }

  // Re-issue the token scoped to another org and reconnect the WS to it.
  async function switchOrg(orgId: string) {
    const { token: newToken } = await api.switchOrg(orgId);
    token.value = newToken;
    activeOrgId.value = orgId;
    // Org is now chosen — commit the token to localStorage and drop the in-memory hold.
    api.setOverrideToken(null);
    api.setToken(newToken);
    wsService.disconnect();
    wsService.connect(newToken);
  }

  async function loadProfile() {
    if (!token.value) return;
    try {
      user.value = await api.getProfile();
    } catch {
      logout();
    }
  }

  function logout() {
    user.value = null;
    token.value = null;
    organizations.value = [];
    activeOrgId.value = null;
    localStorage.removeItem('auth_orgs');
    api.setToken(null);
    wsService.disconnect();
    // Full reload to /login also drops any lingering store state. Matches the
    // 401 interceptor in api.service.ts.
    window.location.href = '/login';
  }

  // Restore session on app start — but only for a still-valid token.
  if (token.value && !isExpired(token.value)) {
    api.setToken(token.value);
    wsService.connect(token.value);   // reconnect WebSocket after page refresh
    loadProfile();
  } else if (token.value) {
    token.value = null;               // drop the stale token so the guard sends us to /login
    api.setToken(null);
  }

  return { user, token, organizations, activeOrgId, loading, error, isAuthenticated, isAdmin, isEditor, login, switchOrg, logout, loadProfile };
});
