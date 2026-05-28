import { ref } from 'vue';

type ToastType = 'success' | 'error';

const visible = ref(false);
const message = ref('');
const type    = ref<ToastType>('success');

let hideTimer: ReturnType<typeof setTimeout> | null = null;

export function useToast() {
  function show(msg: string, toastType: ToastType = 'success', duration = 2500) {
    if (hideTimer) clearTimeout(hideTimer);
    message.value = msg;
    type.value    = toastType;
    visible.value = true;
    hideTimer = setTimeout(() => { visible.value = false; }, duration);
  }

  function hide() {
    if (hideTimer) clearTimeout(hideTimer);
    visible.value = false;
  }

  return { visible, message, type, show, hide };
}
