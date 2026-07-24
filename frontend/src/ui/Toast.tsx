// Bottom-center toast pill with a green check. Auto-dismiss handled by the caller
// (App keeps a timer and clears the message ~1.9s after it is set).
import {CheckIcon} from './icons';

export function Toast({message}: {message: string}) {
  if (!message) {
    return null;
  }
  return (
    <div className="gm-toast" role="status">
      <CheckIcon size={15} stroke="var(--good)" />
      <span>{message}</span>
    </div>
  );
}

export default Toast;
