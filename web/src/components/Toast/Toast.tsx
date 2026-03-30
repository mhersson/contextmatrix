import { useToast, type Toast as ToastType } from '../../hooks/useToast';

const typeStyles: Record<string, { bg: string; border: string; text: string }> = {
  success: {
    bg: 'var(--bg-green)',
    border: 'var(--green)',
    text: 'var(--green)',
  },
  error: {
    bg: 'var(--bg-red)',
    border: 'var(--red)',
    text: 'var(--red)',
  },
  info: {
    bg: 'var(--bg-blue)',
    border: 'var(--aqua)',
    text: 'var(--aqua)',
  },
};

function ToastItem({ toast, onDismiss }: { toast: ToastType; onDismiss: () => void }) {
  const styles = typeStyles[toast.type] || typeStyles.info;

  return (
    <div
      className="flex items-center gap-3 px-4 py-3 rounded-lg shadow-lg border animate-slide-in"
      style={{
        backgroundColor: styles.bg,
        borderColor: styles.border,
      }}
    >
      <span className="text-sm" style={{ color: styles.text }}>
        {toast.message}
      </span>
      <button
        onClick={onDismiss}
        className="ml-2 opacity-70 hover:opacity-100 transition-opacity"
        style={{ color: styles.text }}
      >
        <svg className="w-4 h-4" viewBox="0 0 20 20" fill="currentColor">
          <path
            fillRule="evenodd"
            d="M4.293 4.293a1 1 0 011.414 0L10 8.586l4.293-4.293a1 1 0 111.414 1.414L11.414 10l4.293 4.293a1 1 0 01-1.414 1.414L10 11.414l-4.293 4.293a1 1 0 01-1.414-1.414L8.586 10 4.293 5.707a1 1 0 010-1.414z"
            clipRule="evenodd"
          />
        </svg>
      </button>
    </div>
  );
}

export function ToastContainer() {
  const { toasts, dismissToast } = useToast();

  if (toasts.length === 0) {
    return null;
  }

  return (
    <div className="fixed bottom-4 right-4 z-50 flex flex-col gap-2">
      {toasts.map((toast) => (
        <ToastItem
          key={toast.id}
          toast={toast}
          onDismiss={() => dismissToast(toast.id)}
        />
      ))}
    </div>
  );
}
