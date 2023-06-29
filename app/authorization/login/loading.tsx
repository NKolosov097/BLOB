"use client";

import styles from "./page.module.css";
import { BounceLoader } from "react-spinners";

const Loading = () => {
  return (
    <div className={styles.loading}>
      <BounceLoader color="#d9d9d9" size={100} />;
    </div>
  );
};

export default Loading;
