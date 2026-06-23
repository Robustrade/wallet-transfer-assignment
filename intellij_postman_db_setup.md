# End-to-End Setup Guide: IntelliJ IDEA, Postman & H2 Database

This guide walks you through importing the project into IntelliJ IDEA, running it, setting up Postman to test the APIs, and connecting to the H2 database.

---

## Step 1: Open and Run in IntelliJ IDEA

1. **Import the Project**:
   - Open IntelliJ IDEA.
   - Click **Open** (or **File > Open**).
   - Navigate to and select the directory: `C:\Users\ASUS\OneDrive\Documents\agy2-projects\Wallet Transfer Service`
   - Select **pom.xml** and click **Open as Project**.
   - If prompted, select **Trust Project**.

2. **Wait for Maven Import**:
   - Wait for IntelliJ to download dependencies and index the project (you can monitor progress in the bottom-right status bar).

3. **Configure JDK 21**:
   - Go to **File > Project Structure > Project**.
   - Ensure the **SDK** is set to **Java 21**. If not, select it or choose **Add SDK > Download JDK** to download OpenJDK 21.

4. **Run the Application**:
   - Locate `src/main/java/com/example/wallet/WalletApplication.java` in the Project explorer tree.
   - Right-click `WalletApplication` and select **Run 'WalletApplication.main()'**.
   - The Run window will open at the bottom. Once you see the following log line, the app is active:
     `Tomcat started on port 8080 (http) with context path '/'`

---

## Step 2: Database Setup & Connection

Because we are using **H2 Database** (embedded in-memory), there is **no external database server installation required**. 
- Spring Boot automatically creates the database schema on startup using Hibernate DDL-Auto.
- Initial wallets `wallet_1` ($1000) and `wallet_2` ($500) are seeded automatically.

### Option A: Connecting via IntelliJ IDEA (Ultimate Edition)
If you are using IntelliJ Ultimate, you can connect directly using the built-in Database tool window:
1. Click the **Database** tool window on the far right panel of IntelliJ.
2. Click the **+** (New) button, then select **Data Source > H2**.
3. Configure the connection settings:
   - **Connection Type**: In-memory (or URL)
   - **URL**: `jdbc:h2:mem:walletdb`
   - **User**: `sa`
   - **Password**: *(leave blank)*
4. If prompted to download drivers, click **Download**.
5. Click **Test Connection** (should show Succeeded), then click **OK**.
6. You can now expand the schemas, view tables, and right-click to run query consoles.

### Option B: Connecting via Web H2 Console
If you are using IntelliJ Community Edition:
1. Open your browser and navigate to: [http://localhost:8080/h2-console](http://localhost:8080/h2-console)
2. Enter these parameters:
   - **Driver Class**: `org.h2.Driver`
   - **JDBC URL**: `jdbc:h2:mem:walletdb`
   - **User Name**: `sa`
   - **Password**: *(leave blank)*
3. Click **Connect**.

---

## Step 3: Test Using Postman

1. **Launch Postman**.
2. **Create a New Request**:
   - Click the **+** tab or click **New > HTTP Request**.
3. **Configure Request Method & URL**:
   - Change the method dropdown from `GET` to **`POST`**.
   - URL: `http://localhost:8080/transfers`
4. **Configure Headers**:
   - Select the **Headers** tab.
   - Add a key `Content-Type` with value `application/json`.
5. **Configure Body**:
   - Go to the **Body** tab.
   - Select the **raw** radio button.
   - From the right-most dropdown, choose **JSON**.
   - Paste the following payload into the text area:
     ```json
     {
       "idempotencyKey": "postman-key-001",
       "fromWalletId": "wallet_1",
       "toWalletId": "wallet_2",
       "amount": 100.00
     }
     ```
6. **Send the Request**:
   - Click **Send**.
   - You should receive an **HTTP 201 Created** status code, with a response body showing the transaction status `PROCESSED`.

---

## Step 4: Verify Saved Data (Manually Testing Result)

1. Go back to your **IntelliJ Database Console** or the browser **H2 Console**.
2. Run the following queries to verify the tables updated successfully:
   - Check the new balances (debit/credit applied):
     ```sql
     SELECT * FROM WALLETS;
     ```
     *(`wallet_1` balance should be `900.00` and `wallet_2` balance should be `600.00`)*
   - View the double-entry records:
     ```sql
     SELECT * FROM LEDGER_ENTRIES;
     ```
     *(You should see exactly one DEBIT ledger entry for wallet_1 and one CREDIT ledger entry for wallet_2)*
   - Verify the idempotency record:
     ```sql
     SELECT * FROM IDEMPOTENCY_RECORDS;
     ```
     *(Contains the key `postman-key-001` and the serialized JSON response body)*

3. **Verify Idempotency**:
   - Go back to Postman and click **Send** again with the exact same request body.
   - Notice that the response is returned instantly, the status remains `201 Created`, and if you check your database again, **no additional debit/credit occurs** (wallets balance remains `900` and `600`, and no new ledger rows are written).
