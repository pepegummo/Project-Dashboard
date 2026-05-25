import { defineStore } from 'pinia';
import { ref, computed } from 'vue';
import { api } from '@/services/api.service';
import { wsService } from '@/services/ws.service';
import type { User } from '@/types';

export const useAuthStore = defineStore('auth', () => {
  const user = ref<User | null>(null);
  const token = ref<string | null>(localStorage.getItem('auth_token'));
  const loading = ref(false);
  const error = ref<string | null>(null);

  const isAuthenticated = computed(() => !!token.value && !!user.value);
  const isAdmin = computed(() => user.value?.role === 'admin');
  const isEditor = computed(() => ['admin', 'editor'].includes(user.value?.role ?? ''));

  async function login(email: string, password: string) {
    loading.value = true;
    error.value = null;
    try {
      const result = await api.login(email, password);
      token.value = result.token;
      user.value = result.user;
      api.setToken(result.token);
      wsService.connect(result.token);
      return true;
    } catch (err) {
      error.value = (err as Error).message;
      return false;
    } finally {
      loading.value = false;
    }
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
    api.setToken(null);
    wsService.disconnect();
  }

  // Restore session on app start
  if (token.value) {
    api.setToken(token.value);
    loadProfile();
  }

  return { user, token, loading, error, isAuthenticated, isAdmin, isEditor, login, logout, loadProfile };
});
