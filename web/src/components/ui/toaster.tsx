import { Toaster as HotToaster } from 'react-hot-toast';

const Toaster = () => (
  <HotToaster
    containerClassName="select-text"
    position="bottom-right"
    strictCSP={true}
    toastOptions={{
      duration: 5000,
      className: 'select-text',
    }}
  />
);

export { Toaster };
