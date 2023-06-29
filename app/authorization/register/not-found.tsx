"use client";

import Image from "next/image";
import React from "react";
import styles from "./page.module.css";

const NotFound = () => {
  return (
    <div className={styles.oopss}>
      <div className={styles.errorText}>
        <Image
          src="https://cdn.rawgit.com/ahmedhosna95/upload/1731955f/sad404.svg"
          alt="404"
        />
        <span>404 PAGE</span>
        <p className={styles.pA}>
          . The page you were looking for could not be found
        </p>
        <p className={styles.pB}>... Back to previous page</p>
        <a href="#" className={styles.back}>
          ... Back to previous page
        </a>
      </div>
    </div>
  );
};

export default NotFound;
