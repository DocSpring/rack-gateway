import { Toaster as HotToaster } from 'react-hot-toast'

const Toaster = () => (
  <HotToaster
    strictCSP={true}
    containerClassName="select-text"
    position="bottom-right"
    toastOptions={{
      duration: 5000,
      className: 'select-text',
    }}
  />
)

export { Toaster }
