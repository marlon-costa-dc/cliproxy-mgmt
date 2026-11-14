export const createLatestUsageRequestGuard = () => {
  let latestRequestId = 0;

  return {
    begin: () => ++latestRequestId,
    isLatest: (requestId: number) => requestId === latestRequestId,
    invalidate: () => {
      latestRequestId += 1;
    },
  };
};

export interface LatestUsageRequestOptions<T> {
  guard: ReturnType<typeof createLatestUsageRequestGuard>;
  request: () => Promise<T>;
  fallbackError: string;
  onStart: () => void;
  onSuccess: (payload: T) => void;
  onError: (message: string) => void;
  onFinish: () => void;
}

export const runLatestUsageRequest = async <T>({
  guard,
  request,
  fallbackError,
  onStart,
  onSuccess,
  onError,
  onFinish,
}: LatestUsageRequestOptions<T>): Promise<void> => {
  const requestId = guard.begin();
  onStart();

  try {
    const payload = await request();
    if (guard.isLatest(requestId)) onSuccess(payload);
  } catch (error) {
    if (!guard.isLatest(requestId)) return;
    const message = error instanceof Error ? error.message : String(error);
    const normalizedError = error instanceof Error ? error : new Error(message || fallbackError);
    onError(message || fallbackError);
    throw normalizedError;
  } finally {
    if (guard.isLatest(requestId)) onFinish();
  }
};
