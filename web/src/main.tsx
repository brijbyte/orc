import "./preflight.css";
import "./app.css";

import { createRoot } from "react-dom/client";
import { createBrowserRouter, RouterProvider } from "react-router";
import App, { rootLoader } from "./App";
import { revalidateSoon, setRevalidator } from "./revalidate";
import { SessionRoute } from "./SessionView";
import * as store from "./store";
import "./theme";
import s from "./App.module.css";

const router = createBrowserRouter([
  {
    path: "/",
    Component: App,
    loader: rootLoader,
    children: [
      {
        index: true,
        element: (
          <div className={s.empty}>
            🧌 pick a session on the left, or start one
          </div>
        ),
      },
      {
        path: "s/:sid",
        Component: SessionRoute,
        // seed the session before the route renders; revalidate the
        // sidebar (debounced) once the server reports it live
        loader: ({ params }) =>
          store.ensure(params.sid!, revalidateSoon).then(() => null),
      },
    ],
  },
]);

setRevalidator(() => router.revalidate());

createRoot(document.getElementById("root")!).render(
  <RouterProvider router={router} />,
);
